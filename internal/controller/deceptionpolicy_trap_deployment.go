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

	k8slog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	trapsapi "github.com/dynatrace-oss/koney/internal/controller/traps/api"
	"github.com/dynatrace-oss/koney/internal/controller/traps/filesystoken"
)

// TrapReconcileResult unifies the deployment result after reconciling either decoys or captors.
type TrapReconcileResult struct {
	// NumTraps is the total number of traps that were passed for reconciliation.
	NumTraps int
	// NumSuccesses is the number of traps that were successfully reconciled.
	NumSuccesses int
	// NumFailures is the number of traps that had errors during reconciliation.
	NumFailures int
	// ShouldRequeue is true if we encountered a situation where we should retry the deployment later.
	ShouldRequeue bool
	// OverrideStatusCondition is a reason that should be set when updating the status, instead of the default one.
	OverrideStatusConditionReason string
	// OverrideStatusConditionMessage is a message that should be set when updating the status, instead of the default one.
	OverrideStatusConditionMessage string
	// Errors contains all the errors that happened during the reconciliation.
	Errors error
}

// NumTries is the total number of traps for which we tried a reconciliation (NumSuccesses + NumFailures).
// This number might be lower than NumTraps if we skip traps that don't need to be reconciled.
func (r TrapReconcileResult) NumTries() int {
	return r.NumSuccesses + r.NumFailures
}

// NumSkipped is the number of traps that were skipped during reconciliation.
func (r TrapReconcileResult) NumSkipped() int {
	return r.NumTraps - r.NumSuccesses - r.NumFailures
}

func (r *DeceptionPolicyReconciler) buildFilesystemTokenReconciler(deceptionPolicy *v1alpha1.DeceptionPolicy) filesystoken.FilesystemHoneytokenReconciler {
	return filesystoken.FilesystemHoneytokenReconciler{Client: r.Client, Clientset: r.Clientset, Config: r.Config, DeceptionPolicy: deceptionPolicy}
}

func (r *DeceptionPolicyReconciler) reconcileDecoys(ctx context.Context, deceptionPolicy *v1alpha1.DeceptionPolicy, reconcileTraps []v1alpha1.Trap) TrapReconcileResult {
	log := k8slog.FromContext(ctx)

	results := make([]trapsapi.DecoyDeploymentResult, 0, len(reconcileTraps))
	for _, trap := range reconcileTraps {
		switch trap.TrapType() {
		case v1alpha1.FilesystemHoneytokenTrap:
			rd := r.buildFilesystemTokenReconciler(deceptionPolicy)
			result := rd.DeployDecoy(ctx, deceptionPolicy, trap)
			results = append(results, result)
			if result.GetErrors() != nil {
				log.Error(result.GetErrors(), "FilesystemHoneytoken decoy deployment had errors", "trap", trap.FilesystemHoneytoken)
			}
		case v1alpha1.HttpEndpointTrap:
			log.Error(nil, "HttpEndpointTrap not implemented yet", "trap", trap.HttpEndpoint)
			results = append(results, trapsapi.DecoyDeploymentResult{Trap: &trap, Errors: errors.New("HttpEndpointTrap not implemented yet")})
		case v1alpha1.HttpPayloadTrap:
			log.Error(nil, "HttpPayloadTrap not implemented yet")
			results = append(results, trapsapi.DecoyDeploymentResult{Trap: &trap, Errors: errors.New("HttpPayloadTrap not implemented yet")})
		default:
			log.Error(nil, fmt.Sprintf("trap type %T unknown", trap))
			results = append(results, trapsapi.DecoyDeploymentResult{Trap: &trap, Errors: errors.New("trap type unknown")})
		}
	}

	// Summarize the decoy deployment results
	reconcileResult := TrapReconcileResult{NumTraps: len(reconcileTraps)}
	for _, result := range results {
		result.Errors = errors.Join(result.Errors, result.GetErrors())
		if result.ImpliesFailure() {
			reconcileResult.NumFailures++
		} else if result.ImpliesSuccess() {
			reconcileResult.NumSuccesses++
		}
		if result.ImpliesRetry() {
			log.Info("Encountered resources that are not yet ready for decoys - will retry soon", "trap", result.GetTrap())
			reconcileResult.ShouldRequeue = true
		}
	}

	return reconcileResult
}

func (r *DeceptionPolicyReconciler) reconcileCaptors(ctx context.Context, deceptionPolicy *v1alpha1.DeceptionPolicy, reconcileTraps []v1alpha1.Trap) TrapReconcileResult {
	log := k8slog.FromContext(ctx)

	results := make([]trapsapi.CaptorDeploymentResult, 0, len(reconcileTraps))
	for _, trap := range reconcileTraps {
		switch trap.TrapType() {
		case v1alpha1.FilesystemHoneytokenTrap:
			rd := r.buildFilesystemTokenReconciler(deceptionPolicy)
			result := rd.DeployCaptor(ctx, deceptionPolicy, trap)
			results = append(results, result)
			if result.GetErrors() != nil {
				log.Error(result.GetErrors(), "FilesystemHoneytoken captor deployment had errors", "trap", trap.FilesystemHoneytoken)
			}
		case v1alpha1.HttpEndpointTrap:
			log.Error(nil, "HttpEndpointTrap not implemented yet", "trap", trap.HttpEndpoint)
			results = append(results, trapsapi.CaptorDeploymentResult{Trap: &trap, Errors: errors.New("HttpEndpointTrap not implemented yet")})
		case v1alpha1.HttpPayloadTrap:
			log.Error(nil, "HTTPPayloadTrap not implemented yet")
			results = append(results, trapsapi.CaptorDeploymentResult{Trap: &trap, Errors: errors.New("HTTPPayloadTrap not implemented yet")})
		default:
			log.Error(nil, fmt.Sprintf("trap type %T unknown", trap))
			results = append(results, trapsapi.CaptorDeploymentResult{Trap: &trap, Errors: errors.New("trap type unknown")})
		}
	}

	// Summarize the decoy deployment results
	reconcileResult := TrapReconcileResult{NumTraps: len(reconcileTraps)}
	for _, result := range results {
		result.Errors = errors.Join(result.Errors, result.GetErrors())
		if result.ImpliesFailure() {
			reconcileResult.NumFailures++
		} else if result.ImpliesSuccess() {
			reconcileResult.NumSuccesses++
		}
		if result.MissingTetragon {
			reconcileResult.OverrideStatusConditionReason = CaptorsDeployedReason_MissingTetragon
			reconcileResult.OverrideStatusConditionMessage = CaptorsDeployedMessage_MissingTetragon
		}
		if result.ImpliesRetry() {
			log.Info("Encountered resources that are not yet ready for captors - will retry soon", "trap", result.GetTrap())
			reconcileResult.ShouldRequeue = true
		}
	}

	return reconcileResult
}
