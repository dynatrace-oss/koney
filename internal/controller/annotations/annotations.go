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

package annotations

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8slog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller/constants"
	"github.com/dynatrace-oss/koney/internal/controller/utils"
)

// AddTrapToAnnotations annotates a resource with a deception trap.
// If the trap already exists in the resource annotations, the trap is updated.
// The resource is not updated in the Kubernetes API server,
// the caller is responsible for updating the resource.
func AddTrapToAnnotations(resource client.Object, crdName string, trap v1alpha1.Trap, containers []string) error {
	var oldAnnotationChanges []v1alpha1.ChangeAnnotation // List of changes from the resource annotations
	var newAnnotationChanges []v1alpha1.ChangeAnnotation // List of changes to update the resource annotations

	if existingChanges, ok := resource.GetAnnotations()[constants.AnnotationKeyChanges]; ok {
		if err := json.Unmarshal([]byte(existingChanges), &oldAnnotationChanges); err != nil {
			return err
		}
	}

	// Convert the trap to an annotation trap
	annotationTrap, err := convertTrapToTrapAnnotation(trap, containers)
	if err != nil {
		return err
	}

	changeExists := false
	// Check if the crdName already exists in the changes list
	for _, change := range oldAnnotationChanges {
		if change.DeceptionPolicyName == crdName {
			changeExists = true

			// Check if the trap already exists in the change list
			trapExists := false
			for index, annotationTrap := range change.Traps {
				if AreTheSameTrap(annotationTrap, trap) {
					trapExists = true

					// The trap already exists, update the updatedAt timestamp
					// and the containers list
					change.Traps[index].UpdatedAt = time.Now().Format(time.RFC3339)
					change.Traps[index].Containers = containers

					break
				}
			}

			// If the trap does not exist in the change list, add it
			if !trapExists {
				change.Traps = append(change.Traps, annotationTrap)
			}

			// Add the updated change back to the change list
			newAnnotationChanges = append(newAnnotationChanges, change)
		} else {
			// Add the existing change back to the change list if the crdName does not match
			newAnnotationChanges = append(newAnnotationChanges, change)
		}
	}

	// If the crdName does not exist in the changes list, add a new change
	if !changeExists {
		// Create a new change
		newChange := v1alpha1.ChangeAnnotation{
			DeceptionPolicyName: crdName,
			Traps:               []v1alpha1.TrapAnnotation{annotationTrap},
		}

		// Add the new change to the change list
		newAnnotationChanges = append(newAnnotationChanges, newChange)
	}

	// Marshal the changes to JSON
	changes, err := json.Marshal(newAnnotationChanges)
	if err != nil {
		return err
	}

	// Add the changes to the resource annotations
	if resource.GetAnnotations() == nil {
		resource.SetAnnotations(make(map[string]string))
	}
	resource.GetAnnotations()[constants.AnnotationKeyChanges] = string(changes)

	return nil
}

// UpdateContainersInAnnotations updates the containers list for a deception trap in a resource.
// The resource is not updated in the Kubernetes API server,
// the caller is responsible for updating the resource.
func UpdateContainersInAnnotations(resource client.Object, crdName string, trap v1alpha1.TrapAnnotation, containers []string) error {
	// List of changes from the pod annotations
	var oldAnnotationChanges []v1alpha1.ChangeAnnotation

	if existingChanges, ok := resource.GetAnnotations()[constants.AnnotationKeyChanges]; ok {
		if err := json.Unmarshal([]byte(existingChanges), &oldAnnotationChanges); err != nil {
			return err
		}
	}

	// List of changes to update the pod annotations
	newAnnotationChanges := make([]v1alpha1.ChangeAnnotation, 0, len(oldAnnotationChanges))

	for _, change := range oldAnnotationChanges {
		if change.DeceptionPolicyName == crdName {
			// Check if the trap already exists in the change list
			trapExists := false
			for index, annotationTrap := range change.Traps {
				if annotationTrap.Equals(&trap, true) { // Ignore the containers list when checking for equality
					trapExists = true

					// The trap already exists, update the updatedAt timestamp
					change.Traps[index].UpdatedAt = time.Now().Format(time.RFC3339)
					change.Traps[index].Containers = containers

					break
				}
			}

			// If the trap does not exist in the change list, add it
			if !trapExists {
				trap.CreatedAt = time.Now().Format(time.RFC3339)
				trap.Containers = containers

				change.Traps = append(change.Traps, trap)
			}
		}

		// Add the change back to the updated change list
		newAnnotationChanges = append(newAnnotationChanges, change)
	}

	// Marshal the changes to JSON
	changes, err := json.Marshal(newAnnotationChanges)
	if err != nil {
		return err
	}

	// Add the changes to the pod annotations
	if resource.GetAnnotations() == nil {
		resource.SetAnnotations(make(map[string]string))
	}
	resource.GetAnnotations()[constants.AnnotationKeyChanges] = string(changes)

	return nil
}

// RemoveTrapAnnotations removes a deception trap from a resource.
// The pod is not updated in the Kubernetes API server,
// the caller is responsible for updating the resource.
func RemoveTrapAnnotations(resource client.Object, crdName string, trap v1alpha1.TrapAnnotation) error {
	var oldAnnotationChanges []v1alpha1.ChangeAnnotation // List of changes from the resource annotations
	var newAnnotationChanges []v1alpha1.ChangeAnnotation // List of changes to update the resource annotations

	if existingChanges, ok := resource.GetAnnotations()[constants.AnnotationKeyChanges]; ok {
		if err := json.Unmarshal([]byte(existingChanges), &oldAnnotationChanges); err != nil {
			return err
		}
	}

	for _, change := range oldAnnotationChanges {
		if change.DeceptionPolicyName == crdName {
			var updatedTraps []v1alpha1.TrapAnnotation
			for _, annotationTrap := range change.Traps {
				if !annotationTrap.Equals(&trap, false) { // Do not ignore the containers list when checking for equality
					updatedTraps = append(updatedTraps, annotationTrap)
				}
			}

			change.Traps = updatedTraps
		}

		// If the change still has traps, add it to the updated change list
		if len(change.Traps) > 0 {
			newAnnotationChanges = append(newAnnotationChanges, change)
		}
	}

	// If there are no changes left, remove the annotation
	if len(newAnnotationChanges) == 0 {
		delete(resource.GetAnnotations(), constants.AnnotationKeyChanges)
		return nil
	} else {

		changes, err := json.Marshal(newAnnotationChanges)
		if err != nil {
			return err
		}

		if resource.GetAnnotations() == nil {
			resource.SetAnnotations(make(map[string]string))
		}
		resource.GetAnnotations()[constants.AnnotationKeyChanges] = string(changes)

		return nil
	}
}

// GetAnnotationChange returns the annotation changes for a specific DeceptionPolicy from a resource
func GetAnnotationChange(resource client.Object, crdName string) (v1alpha1.ChangeAnnotation, error) {
	if changes, ok := resource.GetAnnotations()[constants.AnnotationKeyChanges]; ok {
		var annotationChanges []v1alpha1.ChangeAnnotation
		if err := json.Unmarshal([]byte(changes), &annotationChanges); err != nil {
			return v1alpha1.ChangeAnnotation{}, err
		}

		for _, change := range annotationChanges {
			if change.DeceptionPolicyName == crdName {
				return change, nil
			}
		}
	}

	return v1alpha1.ChangeAnnotation{}, nil
}

// AreTheSameTrap returns true if the provided v1alpha1.AnnotationTrap and v1alpha1.Trap are the same.
// This ignores the containers list.
func AreTheSameTrap(annotationTrap v1alpha1.TrapAnnotation, trap v1alpha1.Trap) bool {
	// First, check if the deployment strategy is the same
	if annotationTrap.DeploymentStrategy != trap.DecoyDeployment.Strategy {
		return false
	}

	// Then, check if the trap type is the same
	if annotationTrap.TrapType() != trap.TrapType() {
		return false
	}

	// Finally, check the trap configuration
	switch trap.TrapType() {
	case v1alpha1.FilesystemHoneytokenTrap:
		if annotationTrap.FilesystemHoneytoken.FilePath != trap.FilesystemHoneytoken.FilePath {
			return false
		}
		if annotationTrap.FilesystemHoneytoken.FileContentHash != utils.Hash(trap.FilesystemHoneytoken.FileContent) {
			return false
		}
		if annotationTrap.FilesystemHoneytoken.ReadOnly != trap.FilesystemHoneytoken.ReadOnly {
			return false
		}
	case v1alpha1.HttpEndpointTrap:
		// TODO: Implement.
		return false
	case v1alpha1.HttpPayloadTrap:
		// TODO: Implement.
		return false
	default:
		return false
	}

	return true
}

// GetAnnotatedResources returns a list of resources that have been annotated with a specific DeceptionPolicy.
// Resources whose annotation cannot be parsed are skipped, because we list all pods and deployments
// in the cluster here, and anyone who can annotate a single resource would otherwise stop the
// reconciliation of every DeceptionPolicy, including the clean-up that removes their finalizers.
func GetAnnotatedResources(r client.Reader, ctx context.Context, crdName string) ([]client.Object, error) {
	log := k8slog.FromContext(ctx)

	var annotatedResources []client.Object

	// Get all pods
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods); err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		annotationChange, err := GetAnnotationChange(&pod, crdName)
		if err != nil {
			log.Error(err, "unable to read trap annotations, skipping resource", "pod", pod.Name, "namespace", pod.Namespace)
			continue
		}

		if len(annotationChange.Traps) > 0 {
			annotatedResources = append(annotatedResources, &pod)
		}
	}

	// Get all deployments
	deployments := &appsv1.DeploymentList{}
	if err := r.List(ctx, deployments); err != nil {
		return nil, err
	}

	for _, deployment := range deployments.Items {
		annotationChange, err := GetAnnotationChange(&deployment, crdName)
		if err != nil {
			log.Error(err, "unable to read trap annotations, skipping resource", "deployment", deployment.Name, "namespace", deployment.Namespace)
			continue
		}

		if len(annotationChange.Traps) > 0 {
			annotatedResources = append(annotatedResources, &deployment)
		}
	}

	return annotatedResources, nil
}

func convertTrapToTrapAnnotation(trap v1alpha1.Trap, containers []string) (v1alpha1.TrapAnnotation, error) {
	annotationTrap := v1alpha1.TrapAnnotation{
		DeploymentStrategy: trap.DecoyDeployment.Strategy,
		Containers:         containers,
		CreatedAt:          time.Now().Format(time.RFC3339),
	}

	switch trap.TrapType() {
	case v1alpha1.FilesystemHoneytokenTrap:
		annotationTrap.FilesystemHoneytoken = v1alpha1.FilesystemHoneytokenAnnotation{
			FilePath:        trap.FilesystemHoneytoken.FilePath,
			FileContentHash: utils.Hash(trap.FilesystemHoneytoken.FileContent),
			ReadOnly:        trap.FilesystemHoneytoken.ReadOnly,
		}
	case v1alpha1.HttpEndpointTrap:
		annotationTrap.HttpEndpoint = v1alpha1.HttpEndpointAnnotation{}
	case v1alpha1.HttpPayloadTrap:
		annotationTrap.HttpPayload = v1alpha1.HttpPayloadAnnotation{}
	default:
		return v1alpha1.TrapAnnotation{}, errors.New("unknown trap type")
	}

	return annotationTrap, nil
}
