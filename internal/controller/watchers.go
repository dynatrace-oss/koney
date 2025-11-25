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
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8slog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
)

func HandleWatchEvent(r client.Reader, ctx context.Context, obj client.Object) []reconcile.Request {
	log := k8slog.FromContext(ctx)
	resourceName := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}

	if obj.GetDeletionTimestamp() != nil {
		// Ignore objects that are about to be deleted
		return []reconcile.Request{}
	}

	// For simplicity, just list and then reconcile ALL deception policies (could be optimized)
	deceptionPolicies, err := listAllDeceptionPolicies(r, ctx)
	if err != nil {
		log.Error(err, "Unable to list DeceptionPolicies while watching resource changes")
		return []reconcile.Request{}
	}

	if len(deceptionPolicies) == 0 {
		log.Info(fmt.Sprintf("No DeceptionPolicies must be applied on resource %v", resourceName))
		return []reconcile.Request{}
	}

	reconcileRequests := make([]reconcile.Request, 0, len(deceptionPolicies))
	for _, deceptionPolicy := range deceptionPolicies {
		policyName := types.NamespacedName{Name: deceptionPolicy.Name, Namespace: deceptionPolicy.Namespace}
		request := reconcile.Request{NamespacedName: policyName}
		reconcileRequests = append(reconcileRequests, request)
		log.Info(fmt.Sprintf("Sending reconcile request to %v (triggered by watching %s) ...", deceptionPolicy.Name, resourceName))
	}

	return reconcileRequests
}

func listAllDeceptionPolicies(r client.Reader, ctx context.Context) ([]v1alpha1.DeceptionPolicy, error) {
	deceptionPolicyList := v1alpha1.DeceptionPolicyList{}
	if err := r.List(ctx, &deceptionPolicyList); err != nil {
		return nil, err
	}

	return deceptionPolicyList.Items, nil
}
