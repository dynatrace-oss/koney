// Copyright (c) 2025 Dynatrace LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package controller

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	k8slog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller/constants"
)

// DeceptionPolicyReconciler reconciles a DeceptionPolicy object
type DeceptionPolicyReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Clientset kubernetes.Clientset
	Config    rest.Config
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DeceptionPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (reconcilResult ctrl.Result, reconcileErr error) {
	log := k8slog.FromContext(ctx)
	log.Info("Reconciling DeceptionPolicy ...", "DeceptionPolicy", req.NamespacedName)

	// Fetch the DeceptionPolicy instance
	var deceptionPolicy v1alpha1.DeceptionPolicy
	if err := r.Get(ctx, req.NamespacedName, &deceptionPolicy); err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Info("DeceptionPolicy already deleted - stopping reconciliation", "DeceptionPolicy", req.NamespacedName)
			return ctrl.Result{}, nil
		}

		log.Error(err, "DeceptionPolicy cannot be fetched - stopping reconciliation", "DeceptionPolicy", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// Do not reconcile if the DeceptionPolicy is marked for deletion
	// Run the finalizers to clean-up the deployed traps instead
	markedForDeletion, err := r.runFinalizerIfMarkedForDeletion(ctx, req, &deceptionPolicy)
	if markedForDeletion || err != nil {
		if markedForDeletion {
			if client.IgnoreNotFound(err) == nil {
				log.Info("Finalizer already removed - stopping reconciliation", "DeceptionPolicy", req.NamespacedName)
				return ctrl.Result{}, nil
			}

			log.Info("DeceptionPolicy marked for deletion - stopping reconciliation", "DeceptionPolicy", req.NamespacedName)
		}

		return ctrl.Result{}, err
	}

	missingFinalizer, err := r.putFinalizer(ctx, req, &deceptionPolicy)
	if missingFinalizer || err != nil {
		// We can safely return even if err == nil, another reconciliation request will come,
		// because adding the finalizer also triggered a spec update on the DeceptionPolicy
		if err != nil {
			log.Error(err, "Finalizer cannot be added", "DeceptionPolicy", req.NamespacedName)
		} else {
			log.Info("DeceptionPolicy successfully initialized - will deploy traps next", "DeceptionPolicy", req.NamespacedName)
		}

		return ctrl.Result{}, err
	}

	// Status conditions that are going to be set during the reconciliation
	resourceFoundCondition := v1alpha1.DeceptionPolicyCondition{
		Type:               ResourceFoundType,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             ResourceFoundReason_Found,
		Message:            ResourceFoundMessage_Found,
	}

	policyValidCondition := v1alpha1.DeceptionPolicyCondition{
		Type:               PolicyValidType,
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.Now(),
		Reason:             PolicyValidReason_Pending,
		Message:            "",
	}

	decoysDeployedCondition := v1alpha1.DeceptionPolicyCondition{
		Type:               DecoysDeployedType,
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.Now(),
		Reason:             DecoysDeployedReason_Pending,
		Message:            "",
	}

	captorsDeployedCondition := v1alpha1.DeceptionPolicyCondition{
		Type:               CaptorsDeployedType,
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.Now(),
		Reason:             CaptorsDeployedReason_Pending,
		Message:            "",
	}

	defer func() {
		// Eventually, update status conditions
		err := r.updateStatusConditions(ctx, req, &deceptionPolicy, []v1alpha1.DeceptionPolicyCondition{
			resourceFoundCondition,
			policyValidCondition,
			decoysDeployedCondition,
			captorsDeployedCondition,
		})
		if err != nil {
			log.Error(err, "Status conditions cannot be set", "DeceptionPolicy", req.NamespacedName)
			reconcileErr = errors.Join(reconcileErr, err)
		}
	}()

	// If some traps were removed from the DeceptionPolicy, remove the related deployed decoys and captors
	if err := r.cleanupRemovedTraps(ctx, &deceptionPolicy); err != nil {
		log.Error(err, "Clean-up of traps that were removed failed", "DeceptionPolicy", req.NamespacedName)
		reconcileErr = errors.Join(reconcileErr, err)
		return ctrl.Result{}, reconcileErr
	}

	validTraps := r.filterValidTraps(ctx, &deceptionPolicy)
	numTraps := len(deceptionPolicy.Spec.Traps)
	numTrapsValid := len(validTraps)
	numTrapsInvalid := len(deceptionPolicy.Spec.Traps) - len(validTraps)

	if numTraps > 0 {
		policyValidCondition.Message = fmt.Sprintf("%d/%d traps are valid", len(validTraps), numTraps)
		if numTrapsInvalid > 0 {
			policyValidCondition.Status = metav1.ConditionFalse
			policyValidCondition.Reason = PolicyValidReason_Invalid
		} else {
			policyValidCondition.Status = metav1.ConditionTrue
			policyValidCondition.Reason = PolicyValidReason_Valid
		}
	}

	// Check if strict validation is enabled and we possibly need to stop the reconciliation
	if numTrapsInvalid > 0 {
		if *deceptionPolicy.Spec.StrictValidation {
			log.Info(fmt.Sprintf("DeceptionPolicy has %d invalid traps (out of %d) and strictValidation is enabled - stopping reconciliation", numTrapsInvalid, numTraps), "DeceptionPolicy", req.NamespacedName)
			return ctrl.Result{}, reconcileErr
		} else if !*deceptionPolicy.Spec.StrictValidation && numTrapsValid > 0 {
			log.Info(fmt.Sprintf("DeceptionPolicy has %d invalid traps, which we ignore - continue with %d valid traps", numTrapsInvalid, numTrapsValid), "DeceptionPolicy", req.NamespacedName)
		}
	}

	decoyResult := r.reconcileDecoys(ctx, &deceptionPolicy, validTraps)
	translateReconcileResultToStatusCondition(&decoyResult, &decoysDeployedCondition, DecoyDeployedStatusConditions)

	captorResult := r.reconcileCaptors(ctx, &deceptionPolicy, validTraps)
	translateReconcileResultToStatusCondition(&captorResult, &captorsDeployedCondition, CaptorDeployedStatusConditions)

	// We might encounter resources that are not ready yet, so we should retry later
	shouldRequeue := decoyResult.ShouldRequeue || captorResult.ShouldRequeue

	reconcileErr = errors.Join(reconcileErr, decoyResult.Errors, captorResult.Errors)
	if reconcileErr != nil {
		// If we couldn't deploy all the traps, requeue after a minute to avoid infinite loops
		log.Error(reconcileErr, "Reconciliation failed - check previous logs", "DeceptionPolicy", req.NamespacedName)
		return ctrl.Result{RequeueAfter: constants.NormalFailureRetryInterval}, err
	} else if shouldRequeue {
		// If we encountered resources that are not yet ready for traps, check status again shortly
		log.Info("Reconciliation successful, but some resources are not ready yet - will retry soon", "DeceptionPolicy", req.NamespacedName)
		return ctrl.Result{RequeueAfter: constants.ShortStatusCheckInterval}, nil
	}

	log.Info("Reconciliation successful", "DeceptionPolicy", req.NamespacedName)
	return ctrl.Result{}, reconcileErr
}

func (r *DeceptionPolicyReconciler) runFinalizerIfMarkedForDeletion(ctx context.Context, req ctrl.Request, deceptionPolicy *v1alpha1.DeceptionPolicy) (bool, error) {
	log := k8slog.FromContext(ctx)

	markedForDeletion := deceptionPolicy.GetDeletionTimestamp() != nil
	if markedForDeletion {
		if controllerutil.ContainsFinalizer(deceptionPolicy, constants.FinalizerName) {
			// Run the finalizer to clean-up the deployed traps
			if err := r.cleanupDeceptionPolicy(ctx, deceptionPolicy); err != nil {
				log.Error(err, "Finalizer failed to clean-up traps", "DeceptionPolicy", req.NamespacedName)
				return markedForDeletion, err
			}

			// Remove the finalizer after the clean-up was successful
			err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := r.Get(ctx, req.NamespacedName, deceptionPolicy); err != nil {
					return err
				}
				if dirty := controllerutil.RemoveFinalizer(deceptionPolicy, constants.FinalizerName); !dirty {
					return nil // Already removed
				}
				// TODO: Can we use patch instead of update to avoid conflicts?
				return r.Update(ctx, deceptionPolicy)
			})
			if err != nil {
				return markedForDeletion, err
			}
		}
	}

	return markedForDeletion, nil
}

func (r *DeceptionPolicyReconciler) putFinalizer(ctx context.Context, req ctrl.Request, deceptionPolicy *v1alpha1.DeceptionPolicy) (bool, error) {
	missingFinalizer := !controllerutil.ContainsFinalizer(deceptionPolicy, constants.FinalizerName)
	if missingFinalizer {
		// Add the finalizer if it's missing
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err := r.Get(ctx, req.NamespacedName, deceptionPolicy); err != nil {
				return err
			}
			if dirty := controllerutil.AddFinalizer(deceptionPolicy, constants.FinalizerName); !dirty {
				return nil // Already added
			}
			// TODO: Can we use patch instead of update to avoid conflicts?
			return r.Update(ctx, deceptionPolicy)
		})
		if err != nil {
			return missingFinalizer, err
		}
	}

	return missingFinalizer, nil
}

func (r *DeceptionPolicyReconciler) filterValidTraps(ctx context.Context, deceptionPolicy *v1alpha1.DeceptionPolicy) []v1alpha1.Trap {
	log := k8slog.FromContext(ctx)

	validTraps := make([]v1alpha1.Trap, 0)
	for _, trap := range deceptionPolicy.Spec.Traps {
		if err := trap.IsValid(); err == nil {
			validTraps = append(validTraps, trap)
		} else {
			log.Error(err, "Trap specification invalid", "trap", trap)
		}
	}

	return validTraps
}

func translateReconcileResultToStatusCondition(result *TrapReconcileResult, condition *v1alpha1.DeceptionPolicyCondition, fields TrapDeploymentStatusEnum) {
	if result.NumTraps > 0 {
		condition.Message = fmt.Sprintf("%d/%d %s deployed (%d skipped)", result.NumSuccesses, result.NumTries(), fields.ObjectName, result.NumSkipped())

		if result.NumFailures > 0 || result.Errors != nil {
			condition.Status = metav1.ConditionFalse
			condition.Reason = fields.Reasons.Error
		} else if result.NumTries() == 0 {
			condition.Status = metav1.ConditionFalse
			condition.Reason = fields.Reasons.NoObjects
			condition.Message = fields.Messages.NoObjects
		} else if result.NumSuccesses == result.NumTraps {
			condition.Status = metav1.ConditionTrue
			condition.Reason = fields.Reasons.Success
		} else if result.NumSuccesses == result.NumTries() {
			condition.Status = metav1.ConditionTrue
			condition.Reason = fields.Reasons.PartialSuccess
		}

		// respect overrides
		if result.OverrideStatusConditionReason != "" {
			condition.Reason = result.OverrideStatusConditionReason
		}
		if result.OverrideStatusConditionMessage != "" {
			condition.Message = result.OverrideStatusConditionMessage
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeceptionPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Clientset = *kubernetes.NewForConfigOrDie(mgr.GetConfig())
	r.Config = *mgr.GetConfig()

	watchHandler := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			return HandleWatchEvent(r, ctx, obj)
		})

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DeceptionPolicy{}).
		Named("deceptionpolicy").
		Watches(&corev1.Pod{}, watchHandler).
		Watches(&appsv1.Deployment{}, watchHandler).
		WithEventFilter(predicate.Funcs{
			GenericFunc: func(e event.GenericEvent) bool { return false },
			CreateFunc:  func(e event.CreateEvent) bool { return true },
			UpdateFunc: func(e event.UpdateEvent) bool {
				switch e.ObjectNew.(type) {
				case *corev1.Pod:
				case *appsv1.Deployment:
					// For pods and deployments, consider generation changes and label changes
					// - Generation changes means spec changes, e.g., new container images that need new decoys
					// - Label changes could affect what is matched by the deception policies
					return predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}).Update(e)
				case *v1alpha1.DeceptionPolicy:
					// For deception policies, only consider generation changes
					// (skips update on status, metadata, labels, etc.)
					return predicate.GenerationChangedPredicate{}.Update(e)
				}
				return false
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				switch e.Object.(type) {
				case *corev1.Pod:
				case *appsv1.Deployment:
					// The controller must not change anything when pods or deployments are deleted,
					// only the status conditions will be incorrect until the next periodic reconciliation
					return false
				case *v1alpha1.DeceptionPolicy:
					return true
				}
				return false
			},
		}).
		Complete(r)
}
