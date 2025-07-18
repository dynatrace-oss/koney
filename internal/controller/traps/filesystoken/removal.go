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

package filesystoken

import (
	"context"
	"errors"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller/annotations"
	"github.com/dynatrace-oss/koney/internal/controller/utils"
)

// RemoveDecoy removes a FilesystemHoneytoken decoy from a resource.
// The trap is only removed from the resources where the trap is deployed.
func (r *FilesystemHoneytokenReconciler) RemoveDecoy(ctx context.Context, crdName string, trap v1alpha1.TrapAnnotation, resource client.Object) error {
	log := log.FromContext(ctx)

	var joinedErrors error
	var removedFromContainers []string

	// Remove the trap from the selected container(s)
	for _, containerName := range trap.Containers {
		switch trap.DeploymentStrategy {
		case "containerExec":
			pod := resource.(*corev1.Pod)
			if err := r.removeDecoyWithContainerExec(ctx, trap, *pod, containerName); err != nil {
				log.Error(err, "unable to remove FilesystemHoneytoken trap from container", "container", containerName)
				joinedErrors = errors.Join(joinedErrors, err)
			} else {
				removedFromContainers = append(removedFromContainers, containerName)
			}

		case "volumeMount":
			deployment := resource.(*appsv1.Deployment)
			if err := r.removeDecoyWithVolumeMount(ctx, trap, *deployment, containerName); err != nil {
				log.Error(err, "unable to remove FilesystemHoneytoken trap from container", "container", containerName)
				joinedErrors = errors.Join(joinedErrors, err)
			} else {
				removedFromContainers = append(removedFromContainers, containerName)
			}

		case "kyvernoPolicy":
			log.Info("KyvernoPolicy strategy not implemented yet")
			joinedErrors = errors.New("KyvernoPolicy strategy not implemented yet")
		default:
			log.Error(nil, "unknown strategy", "strategy", trap.DeploymentStrategy)
			joinedErrors = errors.New("unknown strategy")

			return joinedErrors
		}
	}

	// If the file was removed from all containers, remove the trap from the pod annotations
	if len(removedFromContainers) == len(trap.Containers) {
		// Use RetryOnConflict to elegantly avoid conflicts when updating a resource
		// as explained in https://github.com/kubernetes-sigs/controller-runtime/issues/1748
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err := r.Client.Get(ctx, client.ObjectKeyFromObject(resource), resource); err != nil {
				return err
			}

			// Remove the trap from the pod annotations
			err := annotations.RemoveTrapAnnotations(resource, crdName, trap)
			if err != nil {
				log.Error(err, "unable to remove trap from resource annotations", "resource", resource.GetName())
				joinedErrors = errors.Join(joinedErrors, err)
			}

			// TODO: Can we use patch instead of update to avoid conflicts?
			return r.Client.Update(ctx, resource)
		})
		if err != nil {
			log.Error(err, "unable to update resource", "resource", resource.GetName())
			joinedErrors = errors.Join(joinedErrors, err)
		}
	} else {
		// Update the annotation, removing the containers that the trap was removed from
		containersWithTrap := []string{}
		for _, container := range trap.Containers {
			if !utils.Contains(removedFromContainers, container) {
				containersWithTrap = append(containersWithTrap, container)
			}
		}

		// Use RetryOnConflict to elegantly avoid conflicts when updating a resource
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err := r.Client.Get(ctx, client.ObjectKeyFromObject(resource), resource); err != nil {
				return err
			}

			// Update the trap in the pod annotations
			err := annotations.UpdateContainersInAnnotations(resource, crdName, trap, containersWithTrap)
			if err != nil {
				log.Error(err, "unable to update trap in resource annotations", "resource", resource.GetName())
				joinedErrors = errors.Join(joinedErrors, err)
			}

			// TODO: Can we use patch instead of update to avoid conflicts?
			return r.Client.Update(ctx, resource)
		})
		if err != nil {
			log.Error(err, "unable to update resource", "resource", resource.GetName())
			joinedErrors = errors.Join(joinedErrors, err)
		}
	}

	return joinedErrors
}

// removeDecoyWithContainerExec removes a FilesystemHoneytoken trap from a pod using the containerExec strategy.
func (r *FilesystemHoneytokenReconciler) removeDecoyWithContainerExec(ctx context.Context, trap v1alpha1.TrapAnnotation, pod corev1.Pod, containerName string) error {
	log := log.FromContext(ctx)

	var joinedErrors error

	// Remove the file (do not fail if the file is already gone)
	cmd := []string{"rm", "-f", trap.FilesystemHoneytoken.FilePath}
	output, err := r.executeCommandInContainer(ctx, pod, containerName, cmd)
	if err != nil {
		log.Error(err, "unable to remove FilesystemHoneytoken trap from container", "container", containerName, "stderr", output)
		joinedErrors = errors.Join(joinedErrors, err)
	} else {
		// Check if the file was removed
		// ExecCMDInContainer does not run commands in a shell, so we need to use sh -c to do so
		// The command checks if the file exists and prints "File exists" if it does, or "No such file" if it doesn't
		cmd = []string{"sh", "-c", "[ ! -f " + trap.FilesystemHoneytoken.FilePath + " ] && echo 'No such file' || echo 'File exists'"}
		output, err := r.executeCommandInContainer(ctx, pod, containerName, cmd)
		if err != nil {
			log.Error(err, "unable to check if the file was removed", "container", containerName, "stderr", output)
			joinedErrors = errors.Join(joinedErrors, err)
		} else if strings.Contains(output, "No such file") {
			log.Info("FilesystemHoneytoken trap removed from container", "container", containerName)
		} else {
			log.Error(nil, "the file was not removed", "container", containerName)
			joinedErrors = errors.Join(joinedErrors, errors.New("the file was not removed"))
		}
	}

	return joinedErrors
}

// removeDecoyWithVolumeMount removes a FilesystemHoneytoken trap a deployment using the volumeMount strategy.
func (r *FilesystemHoneytokenReconciler) removeDecoyWithVolumeMount(ctx context.Context, trap v1alpha1.TrapAnnotation, deployment appsv1.Deployment, containerName string) error {
	log := log.FromContext(ctx)

	var joinedErrors error

	volumeName := generateVolumeName(trap.FilesystemHoneytoken.FilePath)
	secretName := ""

	// Remove the volume mount from the container
	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == containerName {
			newVolumeMounts := []corev1.VolumeMount{}

			// Remove the volume mount from the container
			for j, volumeMount := range deployment.Spec.Template.Spec.Containers[i].VolumeMounts {
				if volumeMount.Name != volumeName {
					newVolumeMounts = append(newVolumeMounts, deployment.Spec.Template.Spec.Containers[i].VolumeMounts[j])
				} else {
					log.Info("Removing volume mount from container", "volume", volumeName, "container", containerName)
				}
			}

			deployment.Spec.Template.Spec.Containers[i].VolumeMounts = newVolumeMounts
		}
	}

	// Remove the volume from the deployment
	newVolumes := []corev1.Volume{}
	for i, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name != volumeName {
			newVolumes = append(newVolumes, deployment.Spec.Template.Spec.Volumes[i])
		} else {
			secretName = volume.VolumeSource.Secret.SecretName
			log.Info("Removing volume from deployment", "volume", volumeName)
		}
	}
	deployment.Spec.Template.Spec.Volumes = newVolumes

	// Use RetryOnConflict to elegantly avoid conflicts when updating a resource
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// TODO: Can we use patch instead of update to avoid conflicts?
		return r.Client.Update(ctx, &deployment)
	})
	if err != nil {
		log.Error(err, "unable to update pod", "pod", deployment.Name)
		joinedErrors = errors.Join(joinedErrors, err)
	} else {
		log.Info("FilesystemHoneytoken trap removed from container", "container", containerName)
	}

	// Delete the secret, if it was created by the trap
	if secretName != "" {
		secret := corev1.Secret{}
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: deployment.Namespace, Name: secretName}, &secret)
		if err != nil {
			log.Error(err, "unable to get secret", "secret", secretName)
			joinedErrors = errors.Join(joinedErrors, err)
		} else {
			// This might fail if the secret is still being used by another pod, we ignore the error
			_ = r.Client.Delete(ctx, &secret)
		}
	}

	return joinedErrors
}
