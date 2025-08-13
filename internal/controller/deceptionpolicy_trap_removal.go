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

	kivev1 "github.com/San7o/kivebpf/api/v1"
	ciliumiov1alpha1 "github.com/cilium/tetragon/pkg/k8s/apis/cilium.io/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8slog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dynatrace-oss/koney/internal/controller/annotations"
	"github.com/dynatrace-oss/koney/internal/controller/constants"
	"github.com/dynatrace-oss/koney/internal/controller/traps/filesystoken"
	"github.com/dynatrace-oss/koney/internal/controller/utils"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
)

// cleanupDeceptionPolicy cleans up all the traps deployed by a DeceptionPolicy
func (r *DeceptionPolicyReconciler) cleanupDeceptionPolicy(ctx context.Context, deceptionPolicy *v1alpha1.DeceptionPolicy) error {
	// Cycle through the pods and get their annotations
	resources, err := annotations.GetAnnotatedResources(r, ctx, deceptionPolicy.Name)
	if err != nil {
		return err
	}
	for _, resource := range resources {
		annotationChange, err := annotations.GetAnnotationChange(resource, deceptionPolicy.Name)
		if err != nil {
			return err
		}

		// Cycle through the traps and remove them
		for _, trapAnnotation := range annotationChange.Traps {
			if err := r.cleanupTrap(ctx, deceptionPolicy, trapAnnotation, resource); err != nil {
				return err
			}
		}
	}

	return nil
}

// cleanupTrap cleans up a trap from a pod
func (r *DeceptionPolicyReconciler) cleanupTrap(ctx context.Context, deceptionPolicy *v1alpha1.DeceptionPolicy, trapAnnotation v1alpha1.TrapAnnotation, resource client.Object) error {
	switch trapAnnotation.TrapType() {
	case v1alpha1.FilesystemHoneytokenTrap:
		rd := r.buildFilesystemTokenReconciler(deceptionPolicy)
		if err := rd.RemoveDecoy(ctx, deceptionPolicy.Name, trapAnnotation, resource); err != nil {
			return err
		}

	case v1alpha1.HttpEndpointTrap:
		// TODO: Implement.
		return nil
	case v1alpha1.HttpPayloadTrap:
		// TODO: Implement.
		return nil
	default:
		return nil
	}

	return nil
}

// cleanupRemovedTraps cleans up the traps that have been removed from a DeceptionPolicy
func (r *DeceptionPolicyReconciler) cleanupRemovedTraps(ctx context.Context, deceptionPolicy *v1alpha1.DeceptionPolicy) error {
	// Remove the captors
	if err := r.cleanupRemovedCaptors(ctx, deceptionPolicy); err != nil {
		return err
	}

	// Remove the decoys
	if err := r.cleanupRemovedDecoys(ctx, deceptionPolicy); err != nil {
		return err
	}

	return nil
}

// cleanupRemovedCaptors cleans up the captors that have been removed from a DeceptionPolicy
func (r *DeceptionPolicyReconciler) cleanupRemovedCaptors(ctx context.Context, deceptionPolicy *v1alpha1.DeceptionPolicy) error {
	log := k8slog.FromContext(ctx)

	// Tetragon
	isTetragonInstalled := true

	// Get all the TracingPolicies that are associated with this DeceptionPolicy
	// TODO: move this to a function RemoveDecoy in the FilesystemHoneytokenReconciler ?
	tetragonTracingPolicies := &ciliumiov1alpha1.TracingPolicyList{}
	if err := r.Client.List(ctx, tetragonTracingPolicies, client.MatchingLabels{constants.LabelKeyDeceptionPolicyRef: deceptionPolicy.Name}); err != nil {
		// If the error is *meta.NoKindMatchError, ignore it
		_, ok := err.(*meta.NoKindMatchError)
		if !ok {
			return err
		}
		isTetragonInstalled = false
	}

	if isTetragonInstalled {
		tetragonPolicyNamesFromTraps := []string{}
		for _, trap := range deceptionPolicy.Spec.Traps {
			tracingPolicyName, err := filesystoken.GenerateTetragonTracingPolicyName(trap)
			if err != nil {
				return err
			}
			tetragonPolicyNamesFromTraps = append(tetragonPolicyNamesFromTraps, tracingPolicyName)
		}

		notFoundTracingPolicies := []string{}
		for _, tetragonTracingPolicy := range tetragonTracingPolicies.Items {
			if !utils.Contains(tetragonPolicyNamesFromTraps, tetragonTracingPolicy.Name) {
				notFoundTracingPolicies = append(notFoundTracingPolicies, tetragonTracingPolicy.Name)
			}
		}

		if len(notFoundTracingPolicies) > 0 {
			log.Info("Deleting tracing policies for removed traps", "notFoundTracingPolicies", notFoundTracingPolicies)

			// Delete the captor tracing policies that are not found in the DeceptionPolicy
			for _, tracingPolicyName := range notFoundTracingPolicies {
				if err := r.Client.Delete(ctx, &ciliumiov1alpha1.TracingPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name: tracingPolicyName,
					},
				}); err != nil {
					return err
				}
			}
		}
	}

	// Kive

	// Get all the TracingPolicies that are associated with this DeceptionPolicy
	kiveTracingPolicies := &kivev1.KivePolicyList{}
	if err := r.Client.List(ctx, kiveTracingPolicies, client.MatchingLabels{constants.LabelKeyDeceptionPolicyRef: deceptionPolicy.Name}); err != nil {
		// If the error is *meta.NoKindMatchError, ignore it
		if _, ok := err.(*meta.NoKindMatchError); ok {
			// Kive is not installed
			return nil
		}
		return err
	}

	kivePolicyNamesFromTraps := []string{}
	for _, trap := range deceptionPolicy.Spec.Traps {
		tracingPolicyName, err := filesystoken.GenerateKivePolicyName(trap)
		if err != nil {
			return err
		}
		kivePolicyNamesFromTraps = append(kivePolicyNamesFromTraps, tracingPolicyName)
	}

	notFoundTracingPolicies := []string{}
	for _, kiveTracingPolicy := range kiveTracingPolicies.Items {
		if !utils.Contains(kivePolicyNamesFromTraps, kiveTracingPolicy.Name) {
			notFoundTracingPolicies = append(notFoundTracingPolicies, kiveTracingPolicy.Name)
		}
	}

	if len(notFoundTracingPolicies) > 0 {
		log.Info("Deleting tracing policies for removed traps", "notFoundTracingPolicies", notFoundTracingPolicies)

		// Delete the captor tracing policies that are not found in the DeceptionPolicy
		for _, tracingPolicyName := range notFoundTracingPolicies {
			if err := r.Client.Delete(ctx, &kivev1.KivePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tracingPolicyName,
					Namespace: utils.GetKoneyNamespace(),
				},
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

// cleanupRemovedDecoys cleans up the decoys that have been removed from a DeceptionPolicy
func (r *DeceptionPolicyReconciler) cleanupRemovedDecoys(ctx context.Context, deceptionPolicy *v1alpha1.DeceptionPolicy) error {
	// Cycle through the pods and get their annotations
	resources, err := annotations.GetAnnotatedResources(r, ctx, deceptionPolicy.Name)
	if err != nil {
		return err
	}
	for _, resource := range resources {
		annotationChange, err := annotations.GetAnnotationChange(resource, deceptionPolicy.Name)
		if err != nil {
			return err
		}

		// Cycle through the traps and remove them
		for _, trapAnnotation := range annotationChange.Traps {
			// If the trap has been removed from the DeceptionPolicy, remove it
			found := false
			for _, trap := range deceptionPolicy.Spec.Traps {
				if annotations.AreTheSameTrap(trapAnnotation, trap) {
					found = true
					break
				}
			}

			if !found {
				if err := r.cleanupTrap(ctx, deceptionPolicy, trapAnnotation, resource); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
