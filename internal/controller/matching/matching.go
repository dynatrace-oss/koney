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

package matching

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller/utils"
)

type MatchingResult struct {
	// DeployableObjects is a map of objects (pods or deployments) and their containers to which traps can be deployed (running and ready).
	DeployableObjects map[client.Object][]string
	// AtLeastOneObjectWasMatched indicates if we found at least one object in the cluster to which the trap should be deployed to.
	// Matched means that an object matches the trap's selector criteria (regardless of the object's readiness).
	// Note that resources with a deletion timestamp are not even considered for matching, they are treated as if they were not there at all.
	AtLeastOneObjectWasMatched bool
	// AllDeployableObjectsWereReady indicates if all the objects that we wanted to deploy the trap to were ready, or if some were filtered out.
	// If no resources were matched in the first place (i.e., AtLeastOneObjectWasMatched = false), this field should be ignored.
	AllDeployableObjectsWereReady bool
}

// GetDeployableObjectsWithContainers returns a map of resources (pods or deployments) and their containers to which traps can be deployed.
// Deployable objects need to match certain criteria, and not be filtered out. The criteria to match is the following:
// - Only resources (and containers) that match the given MatchResources are returned.
// - Only resources that have no deletion timestamp set are returned.
// - If a createdAfter timestamp is given, only resources created after the given timestamp are returned.
// Additionally, the function filters out resources that are not ready, e.g., pods that are just starting, not ready, or terminating.
//
// The deployment strategy determines which resources are returned: pods (if the strategy is containerExec) or deployments (if the strategy is volumeMount).
// The function returns a matching result and an error. The matching result reports if at least one object matched the three criteria above,
// and if all of those objects were also ready. The final set of deployable objects both matches all criteria and is ready.
func GetDeployableObjectsWithContainers(r client.Reader, ctx context.Context, trap v1alpha1.Trap, createdAfter *metav1.Time) (MatchingResult, error) {
	var (
		matchingObjects map[client.Object][]string
		filteredObjects map[client.Object][]string
		allObjectsReady bool
		err             error
	)

	switch trap.DecoyDeployment.Strategy {
	case "containerExec":
		matchingObjects, err = getMatchingPodsWithContainers(r, ctx, trap.MatchResources)
		matchingObjects = filterObjectsWithoutDeletionTimestamp(matchingObjects)
		if createdAfter != nil {
			matchingObjects = filterObjectsCreatedAfterTimestamp(matchingObjects, *createdAfter)
		}

		filteredObjects, allObjectsReady = filterPodsReadyForTraps(matchingObjects)
	case "volumeMount":
		matchingObjects, err = getMatchingDeploymentsWithContainers(r, ctx, trap.MatchResources)
		matchingObjects = filterObjectsWithoutDeletionTimestamp(matchingObjects)
		if createdAfter != nil {
			matchingObjects = filterObjectsCreatedAfterTimestamp(matchingObjects, *createdAfter)
		}

		filteredObjects, allObjectsReady = filterDeploymentsReadyForTraps(matchingObjects)
	default:
		err = fmt.Errorf("invalid deployment strategy: %s", trap.DecoyDeployment.Strategy)
	}

	if err != nil {
		return MatchingResult{}, err
	}

	// avoid vacuous truth statements, i.e.,
	// if no objects are deployable, then no objects were ready
	// (however, no caller should rely on this field in this case anyway)
	if len(filteredObjects) == 0 {
		allObjectsReady = false
	}

	return MatchingResult{
		DeployableObjects:             filteredObjects,
		AtLeastOneObjectWasMatched:    len(matchingObjects) > 0,
		AllDeployableObjectsWereReady: allObjectsReady,
	}, nil
}

func getMatchingPodsWithContainers(r client.Reader, ctx context.Context, matchResources v1alpha1.MatchResources) (map[client.Object][]string, error) {
	return getMatchingObjectsWithContainers(r, ctx, matchResources, func() client.ObjectList { return &corev1.PodList{} })
}

func getMatchingDeploymentsWithContainers(r client.Reader, ctx context.Context, matchResources v1alpha1.MatchResources) (map[client.Object][]string, error) {
	return getMatchingObjectsWithContainers(r, ctx, matchResources, func() client.ObjectList { return &appsv1.DeploymentList{} })
}

// getMatchingObjectsWithContainers returns a map of objects (pods or deployments) that match the given MatchResources with their containers.
// Resources are matched using with a logical OR between different ResourceFilters and a logical AND between the namespaces and labels of a ResourceFilter.
func getMatchingObjectsWithContainers(r client.Reader, ctx context.Context, matchResources v1alpha1.MatchResources, emptyList func() client.ObjectList) (map[client.Object][]string, error) {
	matchingObjectsWithContainers := map[client.Object][]string{}

	for _, resourceFilter := range matchResources.Any {
		matchingObjects, err := getMatchingObjectsByNamespaceAndLabels(r, ctx, resourceFilter, emptyList)
		if err != nil {
			return nil, err
		}

		for _, matchingObject := range matchingObjects {
			selectedContainers, err := selectContainers(matchingObject, resourceFilter.ContainerSelector)
			if err != nil {
				return nil, err
			} else if len(selectedContainers) == 0 {
				continue // If no containers match the containerSelector, skip the object
			} else {
				// If the object is already in the map, append the selected containers to the existing list (avoiding duplicates)
				objectFromMap := getObjectFromMap(matchingObject.GetName(), matchingObjectsWithContainers)
				if objectFromMap != nil {
					containers := matchingObjectsWithContainers[objectFromMap]

					for _, container := range selectedContainers {
						if !utils.Contains(containers, container) {
							containers = append(containers, container)
						}
					}

					// Add the updated entry to the map
					matchingObjectsWithContainers[objectFromMap] = containers
				} else {
					// Else, create a new entry in the map
					matchingObjectsWithContainers[matchingObject] = selectedContainers
				}
			}
		}
	}

	return matchingObjectsWithContainers, nil
}

// getMatchingObjectsByNamespaceAndLabels returns a list of objects (pods or deployments)
// that match the given resource filter with a logical AND between the namespaces and labels.
func getMatchingObjectsByNamespaceAndLabels(r client.Reader, ctx context.Context, resourceFilter v1alpha1.ResourceFilter, makeList func() client.ObjectList) ([]client.Object, error) {
	matchingObjects := []client.Object{} // The objects that match the MatchResources

	matchingByNamespace := []client.Object{} // The objects that match the namespaces for this ResourceFilter
	matchingByLabels := []client.Object{}    // The objects that match the labels for this ResourceFilter

	if len(resourceFilter.Namespaces) > 0 {
		// Get the objects that match one of the namespaces
		for _, namespace := range resourceFilter.Namespaces {
			items := []client.Object{}
			if err := listItemsAsObjects(r, ctx, &items, makeList(), client.InNamespace(namespace)); err != nil {
				return nil, err
			}

			for _, object := range items {
				if !utils.Contains(extractObjectNames(matchingByNamespace), object.GetName()) {
					matchingByNamespace = append(matchingByNamespace, object)
				}
			}
		}
	}

	if resourceFilter.Selector != nil && len(resourceFilter.Selector.MatchLabels) > 0 {
		// Get the objects that match the labels
		items := []client.Object{}
		if err := listItemsAsObjects(r, ctx, &items, makeList(), client.MatchingLabels(resourceFilter.Selector.MatchLabels)); err != nil {
			return nil, err
		} else {
			for _, object := range items {
				if !utils.Contains(extractObjectNames(matchingByLabels), object.GetName()) {
					matchingByLabels = append(matchingByLabels, object)
				}
			}
		}
	}

	// If no namespaces are specified, add all the objects that match the labels
	if len(resourceFilter.Namespaces) == 0 {
		for _, object := range matchingByLabels {
			if !utils.Contains(extractObjectNames(matchingObjects), object.GetName()) {
				matchingObjects = append(matchingObjects, object)
			}
		}
	}

	// If no labels are specified, add all the objects that match the namespaces
	if resourceFilter.Selector == nil || len(resourceFilter.Selector.MatchLabels) == 0 {
		for _, object := range matchingByNamespace {
			if !utils.Contains(extractObjectNames(matchingObjects), object.GetName()) {
				matchingObjects = append(matchingObjects, object)
			}
		}
	}

	// If both namespaces and labels are specified, add the objects that match both (logical AND between namespaces and labels)
	for _, objectByNamespace := range matchingByNamespace {
		for _, objectByLabels := range matchingByLabels {
			if objectByNamespace.GetName() == objectByLabels.GetName() {
				if !utils.Contains(extractObjectNames(matchingObjects), objectByNamespace.GetName()) {
					matchingObjects = append(matchingObjects, objectByNamespace)
				}
			}
		}
	}

	return matchingObjects, nil
}

// filterObjectsWithoutDeletionTimestamp only keeps objects that have no deletion timestamp set.
func filterObjectsWithoutDeletionTimestamp[T any](objects map[client.Object]T) map[client.Object]T {
	filteredObjects := map[client.Object]T{}
	for object, value := range objects {
		if object.GetDeletionTimestamp() == nil {
			filteredObjects[object] = value
		}
	}
	return filteredObjects
}

// filterObjectsCreatedAfterTimestamp only keeps objects that were created at or after the given timestamp.
func filterObjectsCreatedAfterTimestamp[T any](objects map[client.Object]T, policyCreatedAt metav1.Time) map[client.Object]T {
	filteredObjects := map[client.Object]T{}
	for object, value := range objects {
		objectCreatedAt := object.GetCreationTimestamp()
		if policyCreatedAt.Before(&objectCreatedAt) {
			filteredObjects[object] = value
		}
	}
	return filteredObjects
}

// filterPodsReadyForTraps only keeps pods that are running, and for each pod, only containers that are running and ready.
// The function returns a filtered map, and a boolean that is only true if no pod or container was filtered out.
func filterPodsReadyForTraps(objects map[client.Object][]string) (map[client.Object][]string, bool) {
	filteredObjects := map[client.Object][]string{}
	allContainersReady := true

	for pod, containers := range objects {
		if pod, ok := pod.(*corev1.Pod); ok {
			if pod.Status.Phase != corev1.PodRunning {
				allContainersReady = false
				continue // skip entire pod
			}

			if utils.GetPodCondition(&pod.Status.Conditions, corev1.ContainersReady) != corev1.ConditionTrue {
				allContainersReady = false // flag as not ready, but still checking individual containers
			}

			for _, status := range pod.Status.ContainerStatuses {
				if !utils.Contains(containers, status.Name) {
					continue // ignore, name not even matching
				}
				if status.State.Running == nil || !status.Ready {
					allContainersReady = false
					continue // skip this container
				}

				filteredObjects[pod] = append(filteredObjects[pod], status.Name)
			}
		}
	}

	return filteredObjects, allContainersReady
}

// filterDeploymentsReadyForTraps only keeps deployments that have the Available condition set to True. The list of containers is not filtered.
// The function returns the filtered map, and a boolean that is only true if no deployment was filtered out.
func filterDeploymentsReadyForTraps(objects map[client.Object][]string) (map[client.Object][]string, bool) {
	filteredObjects := map[client.Object][]string{}
	allDeploymentsReady := true

	for deployment, containers := range objects {
		if deployment, ok := deployment.(*appsv1.Deployment); ok {
			if utils.GetDeploymentCondition(&deployment.Status.Conditions, appsv1.DeploymentAvailable) != corev1.ConditionTrue {
				allDeploymentsReady = false
				continue // skip entire deployment
			}

			filteredObjects[deployment] = containers
		}
	}

	return filteredObjects, allDeploymentsReady
}

// selectContainers selects the container(s) in a Kubernetes resource based
// on the containerSelector. containerSelector can be a wildcard
// and can include wildcards inside the string.
// The function returns a list of container names that match the selector.
func selectContainers(resource client.Object, containerSelector string) ([]string, error) {
	var containers []corev1.Container
	switch resource := resource.(type) {
	case *corev1.Pod:
		containers = resource.Spec.Containers
	case *appsv1.Deployment:
		containers = resource.Spec.Template.Spec.Containers
	default:
		return nil, fmt.Errorf("invalid resource type: %T", resource)
	}

	selectedContainers := []string{}

	if ContainerSelectorSelectsAll(containerSelector) {
		for _, container := range containers {
			selectedContainers = append(selectedContainers, container.Name)
		}
		return selectedContainers, nil
	}

	for _, container := range containers {

		matched, err := utils.MatchContainerName(containerSelector, container.Name)
		if err != nil {
			return []string{}, fmt.Errorf("invalid container selector: %w", err)
		}

		if matched {
			selectedContainers = append(selectedContainers, container.Name)
		}
	}

	return selectedContainers, nil
}

func listItemsAsObjects(r client.Reader, ctx context.Context, items *[]client.Object, list client.ObjectList, opts ...client.ListOption) error {
	if err := r.List(ctx, list, opts...); err != nil {
		return err
	}

	// we need to duplicate code because PodList and DeploymentList do not share a common interface
	switch v := list.(type) {
	case *corev1.PodList:
		for _, item := range v.Items {
			*items = append(*items, &item)
		}
	case *appsv1.DeploymentList:
		for _, item := range v.Items {
			*items = append(*items, &item)
		}
	}

	return nil
}
