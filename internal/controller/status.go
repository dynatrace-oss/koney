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

	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
)

const (
	ResourceFoundType   = "ResourceFound"
	PolicyValidType     = "PolicyValid"
	DecoysDeployedType  = "DecoysDeployed"
	CaptorsDeployedType = "CaptorsDeployed"

	ResourceFoundReason_Found = "ResourceFound"

	ResourceFoundMessage_Found = "DeceptionPolicy found and ready"

	PolicyValidReason_Pending = "ValidationPending"
	PolicyValidReason_Valid   = "TrapsSpecValid"
	PolicyValidReason_Invalid = "TrapsSpecInvalid"

	DecoysDeployedReason_Pending        = "DecoyDeploymentPending"
	DecoysDeployedReason_Success        = "DecoyDeploymentSucceeded"
	DecoysDeployedReason_PartialSuccess = "DecoyDeploymentSucceededPartially"
	DecoysDeployedReason_GenericError   = "DecoyDeploymentError"
	DecoysDeployedReason_NoObjects      = "NoObjectsMatched"

	TrapDeployedMessage_NoObjects = "No objects matching selection criteria"

	CaptorsDeployedReason_Pending         = "CaptorDeploymentPending"
	CaptorsDeployedReason_Success         = "CaptorDeploymentSucceeded"
	CaptorsDeployedReason_PartialSuccess  = "CaptorDeploymentSucceededPartially"
	CaptorsDeployedReason_GenericError    = "CaptorDeploymentError"
	CaptorsDeployedReason_NoObjects       = "NoObjectsMatched"
	CaptorsDeployedReason_MissingTetragon = "TetragonNotInstalled"

	CaptorsDeployedMessage_MissingTetragon = "Cannot deploy captors without Tetragon"
)

// TrapDeploymentStatusEnum defines the possible conditions for a trap deployment.
// This struct exists so that we can generically pass decoy and captor status conditions.
type TrapDeploymentStatusEnum struct {
	// ObjectName is the name of the traps being deployed (e.g. "decoys" or "captors").
	ObjectName string
	// Reasons contains the possible reasons for the trap deployment status.
	Reasons TrapDeploymentStatusReasonsEnum
	// Messages contains the possible messages for the trap deployment status.
	Messages TrapDeploymentStatusMessagesEnum
}

type TrapDeploymentStatusReasonsEnum struct {
	Unknown        string
	Success        string
	Error          string
	PartialSuccess string
	NoObjects      string
}

type TrapDeploymentStatusMessagesEnum struct {
	NoObjects string
}

// DecoyDeployedStatusConditions stores the status condition reasons and messages for decoys.
var DecoyDeployedStatusConditions = TrapDeploymentStatusEnum{
	ObjectName: "decoys",
	Reasons: TrapDeploymentStatusReasonsEnum{
		Unknown:        DecoysDeployedReason_Pending,
		Success:        DecoysDeployedReason_Success,
		PartialSuccess: DecoysDeployedReason_PartialSuccess,
		Error:          DecoysDeployedReason_GenericError,
		NoObjects:      DecoysDeployedReason_NoObjects,
	},
	Messages: TrapDeploymentStatusMessagesEnum{
		NoObjects: TrapDeployedMessage_NoObjects,
	},
}

// CaptorDeployedStatusConditions stores the status condition reasons and messages for captors.
var CaptorDeployedStatusConditions = TrapDeploymentStatusEnum{
	ObjectName: "captors",
	Reasons: TrapDeploymentStatusReasonsEnum{
		Unknown:        CaptorsDeployedReason_Pending,
		Success:        CaptorsDeployedReason_Success,
		PartialSuccess: CaptorsDeployedReason_PartialSuccess,
		Error:          CaptorsDeployedReason_GenericError,
		NoObjects:      CaptorsDeployedReason_NoObjects,
	},
	Messages: TrapDeploymentStatusMessagesEnum{
		NoObjects: TrapDeployedMessage_NoObjects,
	},
}

// updateStatusConditions updates one or more conditions of a DeceptionPolicy resource.
// If the conditions are already set as desired, no update is performed.
// When comparing the current and desired conditions, the LastTransitionTime field is ignored.
// This function retries on conflicts (to resolve parallel update attempts) and returns an error if the update fails.
func (r *DeceptionPolicyReconciler) updateStatusConditions(ctx context.Context, req ctrl.Request, deceptionPolicy *v1alpha1.DeceptionPolicy, conditions []v1alpha1.DeceptionPolicyCondition) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Get(ctx, req.NamespacedName, deceptionPolicy); err != nil {
			return err
		}

		anyDirty := false
		for _, condition := range conditions {
			dirty := deceptionPolicy.Status.PutCondition(condition.Type, condition.Status, condition.Reason, condition.Message)
			anyDirty = anyDirty || dirty
		}
		if !anyDirty {
			return nil // All conditions already have their desired values
		}

		// TODO: Can we use patch instead of update to avoid conflicts?
		err := r.Status().Update(ctx, deceptionPolicy)
		return err
	})
}
