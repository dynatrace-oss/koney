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
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	ciliumiov1alpha1 "github.com/cilium/tetragon/pkg/k8s/apis/cilium.io/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8slog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller/annotations"
	"github.com/dynatrace-oss/koney/internal/controller/constants"
	"github.com/dynatrace-oss/koney/internal/controller/matching"
	trapsapi "github.com/dynatrace-oss/koney/internal/controller/traps/api"
	"github.com/dynatrace-oss/koney/internal/controller/utils"
)

type FilesystemHoneytokenReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Clientset kubernetes.Clientset
	Config    rest.Config

	DeceptionPolicy *v1alpha1.DeceptionPolicy
}

// DeployDecoy deploys a FilesystemHoneytoken decoy.
// The trap is only deployed to the resources where the trap is not already deployed.
// The boolean return type indicates if any of the resources was not ready yet and this function should be called again later.
func (r *FilesystemHoneytokenReconciler) DeployDecoy(ctx context.Context, deceptionPolicy *v1alpha1.DeceptionPolicy, trap v1alpha1.Trap) trapsapi.DecoyDeploymentResult {
	log := k8slog.FromContext(ctx)
	var joinedErrors error

	// If we aren't allowed to mutate existing resources, we avoid matching resources created before the policy was created
	var filterCreatedAfter metav1.Time
	if !*deceptionPolicy.Spec.MutateExisting {
		filterCreatedAfter = deceptionPolicy.CreationTimestamp
	}

	// Get matching resources and the matched containers: pods for containerExec, deployments for volumeMount
	matchingResult, err := matching.GetDeployableObjectsWithContainers(r, ctx, trap, &filterCreatedAfter)
	if err != nil {
		log.Error(err, "unable to get matching resources")
		// wrap error with message "unable to get matching resources"
		return trapsapi.DecoyDeploymentResult{Errors: errors.Join(err, errors.New("unable to get matching resources"))}
	} else if len(matchingResult.DeployableObjects) == 0 {
		return trapsapi.DecoyDeploymentResult{
			AtLeastOneObjectsWasMatched: matchingResult.AtLeastOneObjectWasMatched,
			AllObjectsWereReady:         matchingResult.AllDeployableObjectsWereReady}
	}

	// Deploy the trap to the matching resources
	for resource, selectedContainers := range matchingResult.DeployableObjects {
		// Check if the trap was already deployed to the resource (and to which containers)
		// Get the resource's changes annotation
		changes, err := annotations.GetAnnotationChange(resource, deceptionPolicy.Name) // Empty if the annotation does not exist
		if err != nil {
			log.Error(err, "unable to get annotation changes")
			joinedErrors = errors.Join(joinedErrors, err)
			continue
		}

		var alreadyDeployedToContainers []string // Containers where the trap was already deployed
		var deployedToContainers []string        // Containers where at the end of the function the trap is deployed to

		// Cycle through the traps in the annotation
		for _, annotationTrap := range changes.Traps {
			// Are areTheSameTrap checks if two traps are the same, ignoring the containers field
			// since Trap does not have a list of containers, but only a containerSelector
			if annotations.AreTheSameTrap(annotationTrap, trap) {
				// The trap was already deployed to the containers in the annotation
				alreadyDeployedToContainers = append(alreadyDeployedToContainers, annotationTrap.Containers...)
			}
		}

		// Deploy the trap to the selected container(s)
		for _, containerName := range selectedContainers {
			if utils.Contains(alreadyDeployedToContainers, containerName) {
				log.Info("FilesystemHoneytoken trap already deployed to container", "resource", resource.GetName(), "container", containerName)

				// We need to add it here regardless to update the annotation
				// Note that, since we are cycling through the selected containers,
				// this will not add containers where the trap was already deployed but that do not exist anymore
				deployedToContainers = append(deployedToContainers, containerName)
				continue
			}

			// Deploy the trap to the container
			switch trap.DecoyDeployment.Strategy {
			case "containerExec":
				// The containerExec strategy deploys the honeytoken directly to containers inside a pod
				if pod, ok := resource.(*corev1.Pod); ok {
					if err := r.deployDecoyWithContainerExec(ctx, trap, *pod, containerName); err != nil {
						log.Error(err, "unable to deploy FilesystemHoneytoken trap to container with containerExec strategy", "container", containerName)
						joinedErrors = errors.Join(joinedErrors, err)
					} else {
						deployedToContainers = append(deployedToContainers, containerName)
					}
				}

			case "volumeMount":
				// The volumeMount strategy deploys the honeytoken mounting a volume in the deployment to the containers
				if deployment, ok := resource.(*appsv1.Deployment); ok {
					if err := r.deployDecoyWithVolumeMount(ctx, trap, *deployment, containerName); err != nil {
						log.Error(err, "unable to deploy FilesystemHoneytoken trap to container with volumeMount strategy", "container", containerName)
						joinedErrors = errors.Join(joinedErrors, err)
					} else {
						deployedToContainers = append(deployedToContainers, containerName)
					}
				}

			case "kyvernoPolicy":
				log.Info("KyvernoPolicy strategy not implemented yet")
				joinedErrors = errors.Join(joinedErrors, errors.New("KyvernoPolicy strategy not implemented yet"))
			default:
				log.Error(nil, "unknown strategy", "strategy", trap.DecoyDeployment.Strategy)
				joinedErrors = errors.Join(joinedErrors, errors.New("unknown strategy"))
			}
		}

		// Annotate the pod with the trap
		if len(deployedToContainers) > 0 {
			// Use RetryOnConflict to elegantly avoid conflicts when updating a resource
			err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := r.Get(ctx, client.ObjectKeyFromObject(resource), resource); err != nil {
					return err
				}

				// Add the trap to the pod annotations
				err := annotations.AddTrapToAnnotations(resource, deceptionPolicy.Name, trap, deployedToContainers)
				if err != nil {
					log.Error(err, "unable to add trap to resource annotations", "resource", resource.GetName())
					joinedErrors = errors.Join(joinedErrors, err)
				}

				// TODO: Can we use patch instead of update to avoid conflicts?
				return r.Update(ctx, resource)
			})
			if err != nil {
				log.Error(err, "unable to update resource", "resource", resource.GetName())
				joinedErrors = errors.Join(joinedErrors, err)
			}
		}
	}

	return trapsapi.DecoyDeploymentResult{
		AtLeastOneObjectsWasMatched: matchingResult.AtLeastOneObjectWasMatched,
		AllObjectsWereReady:         matchingResult.AllDeployableObjectsWereReady,
		Errors:                      joinedErrors}
}

// DeployCaptor deploys a captor for a filesystem honeytoken trap.
func (r *FilesystemHoneytokenReconciler) DeployCaptor(ctx context.Context, deceptionPolicy *v1alpha1.DeceptionPolicy, trap v1alpha1.Trap) trapsapi.CaptorDeploymentResult {
	log := k8slog.FromContext(ctx)

	switch trap.CaptorDeployment.Strategy {
	case "tetragon":
		if err := r.deployCaptorWithTetragon(ctx, deceptionPolicy, trap); err != nil {
			missingTetragon := errors.Is(err, &meta.NoKindMatchError{})
			if missingTetragon {
				log.Error(nil, "Tetragon is not installed - cannot deploy captors with Tetragon")
			}
			return trapsapi.CaptorDeploymentResult{Trap: &trap, Errors: err, MissingTetragon: missingTetragon}
		}
	case "kive":
		if err := r.deployCaptorWithKive(ctx, deceptionPolicy, trap); err != nil {
			missingKive := errors.Is(err, &meta.NoKindMatchError{})
			if missingKive {
				log.Error(nil, "Kive is not installed - cannot deploy captors with Kive")
			}
			return trapsapi.CaptorDeploymentResult{Trap: &trap, Errors: err, MissingTetragon: missingKive}
		}
	default:
		log.Error(nil, fmt.Sprintf("captor deployment strategy '%s' unknown", trap.CaptorDeployment.Strategy))
		return trapsapi.CaptorDeploymentResult{Trap: &trap, Errors: errors.New("captor deployment strategy unknown")}
	}

	return trapsapi.CaptorDeploymentResult{Trap: &trap}
}

// deployDecoyWithContainerExec deploys a FilesystemHoneytoken trap to a list of pods using the containerExec strategy.
// The trap is only deployed to the pods where the trap is not already deployed.
func (r *FilesystemHoneytokenReconciler) deployDecoyWithContainerExec(ctx context.Context, trap v1alpha1.Trap, pod corev1.Pod, containerName string) error {
	log := k8slog.FromContext(ctx)

	var joinedErrors error
	var cmd []string

	// Create the directory if it doesn't exist
	directory := trap.FilesystemHoneytoken.FilePath[:strings.LastIndex(trap.FilesystemHoneytoken.FilePath, "/")]
	cmd = []string{"mkdir", "-p", directory}
	_, err := r.executeCommandInContainer(ctx, pod, containerName, cmd)
	if err != nil {
		log.Error(err, "unable to create directory with mkdir in container", "directory", directory, "container", containerName)
		joinedErrors = errors.Join(joinedErrors, err)

		return joinedErrors
	}

	// mark the commands with a fingerprint so that we won't alert on them later
	echoFingerprint := utils.EncodeFingerprintInEcho(utils.KoneyFingerprint)
	catFingerprint := utils.EncodeFingerprintInCat(utils.KoneyFingerprint)

	if trap.FilesystemHoneytoken.FileContent != "" {
		// To avoid issues with special characters (e.g., command injection vulnerabilities),
		// we first encode the content in octal (sh does not like hex) and then decode it in the container
		octalContent := utils.StringToOct(trap.FilesystemHoneytoken.FileContent)

		// To decode the octal content, we use the following command:
		// oct_string="141142143"; i=1; while [ $i -lt ${#oct_string} ]; do $(which echo) -e "\0$(expr substr $oct_string $i 3)\c"; i=$(expr $i + 3); done > /path/to/file
		// $(which echo) is used to avoid issues with the shell built-in echo command
		cmd = []string{"sh", "-c", "oct_string=\"" + octalContent + "\"; i=1; while [ $i -lt ${#oct_string} ]; do $(which echo) -e \"\\0$(expr substr $oct_string $i 3)\\c " + echoFingerprint + "\"; i=$(expr $i + 3); done > \"" + trap.FilesystemHoneytoken.FilePath + "\""}
	} else {
		// We don't use touch because if the file already includes content, touch would not make it empty
		cmd = []string{"sh", "-c", "echo -e \"\\c " + echoFingerprint + "\" > \"" + trap.FilesystemHoneytoken.FilePath + "\""}
	}

	// Use ExecCMDInContainer to execute the command in the container
	output, err := r.executeCommandInContainer(ctx, pod, containerName, cmd)
	if err != nil {
		log.Error(err, "unable to deploy FilesystemHoneytoken trap to container", "container", containerName, "stderr", output)
		// We don't return here to try to deploy the trap to the other containers
		joinedErrors = errors.Join(joinedErrors, err)

		return joinedErrors
	} else {
		// Check if the file was created with the expected content
		cmd = []string{"sh", "-c", "cat " + catFingerprint + " \"" + trap.FilesystemHoneytoken.FilePath + "\""}
		output, err := r.executeCommandInContainer(ctx, pod, containerName, cmd)
		if err != nil {
			log.Error(err, "unable to read the content of the file", "container", containerName)
			joinedErrors = errors.Join(joinedErrors, err)
		} else if strings.TrimSuffix(output, "\n") != strings.TrimSuffix(trap.FilesystemHoneytoken.FileContent, "\n") { // TrimSuffix removes the trailing newline
			log.Error(nil, "the content of the file is not the expected content", "container", containerName, "expected", trap.FilesystemHoneytoken.FileContent, "actual", output)
			joinedErrors = errors.Join(joinedErrors, errors.New("the content of the file is not the expected content"))
		} else {
			log.Info("FilesystemHoneytoken trap deployed to container", "container", containerName)
		}

		if trap.FilesystemHoneytoken.ReadOnly {
			cmd = []string{"chmod", "444", trap.FilesystemHoneytoken.FilePath}
			_, err = r.executeCommandInContainer(ctx, pod, containerName, cmd)
			if err != nil {
				log.Error(err, "unable to make the file read-only", "container", containerName)
				joinedErrors = errors.Join(joinedErrors, err)
			}
		}
	}

	return joinedErrors
}

// deployDecoyWithVolumeMount deploys a FilesystemHoneytoken trap to
// a list of deployments using the volumeMount strategy.
// The trap is only deployed to the pods where the trap is not already deployed.
func (r *FilesystemHoneytokenReconciler) deployDecoyWithVolumeMount(ctx context.Context, trap v1alpha1.Trap, deployment appsv1.Deployment, containerName string) error {
	log := k8slog.FromContext(ctx)

	var joinedErrors error

	// The name of the secret is generated based on the trap's file path and content
	secretName := generateSecretName(trap)

	mountPath, fileName := filepath.Split(trap.FilesystemHoneytoken.FilePath)
	if fileName == "" {
		log.Error(nil, "file path must point to a file", "file path", trap.FilesystemHoneytoken.FilePath)
		return errors.New("file path must point to a file")
	}

	data := map[string][]byte{
		fileName: []byte(trap.FilesystemHoneytoken.FileContent),
	}

	if err := createSecret(r.Client, ctx, deployment.Namespace, secretName, data); err != nil {
		log.Error(err, "unable to create secret", "secret", secretName)
		joinedErrors = errors.Join(joinedErrors, err)

		return joinedErrors
	}

	// The name of the volume is generated based on the trap's file path
	// For the volume name, we don't need to also consider the content of the file
	// since there cannot be two volumes mounted to the same path with different content
	volumeName := generateVolumeName(trap.FilesystemHoneytoken.FilePath)

	// Get the pod
	if err := r.Get(ctx, client.ObjectKeyFromObject(&deployment), &deployment); err != nil {
		log.Error(err, "unable to get deployment", "deployment", deployment.Name)
		joinedErrors = errors.Join(joinedErrors, err)
	}

	// Check if the volume is already configured to the deployment
	volumeAlreadyConfigured := false
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name == volumeName {
			volumeAlreadyConfigured = true
			break
		}
	}

	if volumeAlreadyConfigured {
		log.Info("Volume already configured", "volume", volumeName)
	} else {
		// Add the volume to the deployment
		deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
				},
			},
		})
	}

	// Add the volume mount to the container
	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == containerName {
			// Check if the volume is already mounted
			volumeAlreadyMounted := false
			for _, volumeMount := range deployment.Spec.Template.Spec.Containers[i].VolumeMounts {
				if volumeMount.Name == volumeName {
					volumeAlreadyMounted = true
					break
				}
			}

			if !volumeAlreadyMounted {
				log.Info("Adding volume mount to container", "container", containerName, "volume", volumeName, "mountPath", mountPath)
				deployment.Spec.Template.Spec.Containers[i].VolumeMounts = append(deployment.Spec.Template.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{
					Name:      volumeName,
					MountPath: trap.FilesystemHoneytoken.FilePath,
					ReadOnly:  trap.FilesystemHoneytoken.ReadOnly,
					SubPath:   fileName,
				})
			}
		}
	}

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// TODO: Can we use patch instead of update to avoid conflicts?
		return r.Update(ctx, &deployment)
	})
	if err != nil {
		log.Error(err, "unable to update deployment", "deployment", deployment.Name)
		joinedErrors = errors.Join(joinedErrors, err)
	} else {
		log.Info("FilesystemHoneytoken trap deployed to container", "container", containerName)
	}

	return joinedErrors
}

// deployCaptorWithTetragon generates a Tetragon tracing policy
// to trace the filesystem access of a filesystem honeytoken trap and applies it to the cluster.
func (r *FilesystemHoneytokenReconciler) deployCaptorWithTetragon(ctx context.Context, deceptionPolicy *v1alpha1.DeceptionPolicy, trap v1alpha1.Trap) error {
	log := k8slog.FromContext(ctx)

	tracingPolicyName, err := GenerateTetragonTracingPolicyName(trap)
	if err != nil {
		log.Error(err, "unable to generate Tetragon tracing policy name")
		return err
	}

	// Get the Tetragon tracing policy if it already exists
	// If the tracing policy already exists, we don't need to do anything
	// since the name is unique for each unique trap
	existingTracingPolicy := &ciliumiov1alpha1.TracingPolicy{}
	err = r.Get(ctx, client.ObjectKey{Name: tracingPolicyName}, existingTracingPolicy)

	// If the policy does not exist, err is not nil and is a NotFound error
	if err != nil {
		// If the policy does not exist, we create it
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to get Tetragon tracing policy")
			return err
		}

		tracingPolicy := generateTetragonTracingPolicy(deceptionPolicy, trap, tracingPolicyName)

		if err := r.Create(ctx, tracingPolicy); err != nil {
			log.Error(err, "unable to create Tetragon tracing policy")
			return err
		}

		log.Info("Tetragon tracing policy created", "policy", tracingPolicy)
	}

	return nil
}

// deployCaptorWithKive generates a Kive tracing policy
// to trace the filesystem access of a filesystem honeytoken trap and applies it to the cluster.
func (r *FilesystemHoneytokenReconciler) deployCaptorWithKive(ctx context.Context, deceptionPolicy *v1alpha1.DeceptionPolicy, trap v1alpha1.Trap) error {
	log := k8slog.FromContext(ctx)

	tracingPolicyName, err := GenerateKivePolicyName(trap)
	if err != nil {
		log.Error(err, "unable to generate Kive tracing policy name")
		return err
	}

	tracingPolicy := generateKivePolicy(deceptionPolicy, trap, tracingPolicyName)
	if err != nil {
		log.Error(err, "unable to generate Kive tracing policy")
		return err
	}

	if err := r.Patch(ctx, tracingPolicy, client.Apply, client.ForceOwnership, client.FieldOwner(constants.FieldOwnerKoneyController)); err != nil {
		log.Error(err, "unable to create Kive tracing policy")
		return err
	}

	log.Info("Kive tracing policy created", "policy", tracingPolicy)

	return nil
}

// executeCommandInContainer executes a command in a container. If the command
// is successful, the function returns the stdout output. If the command
// fails, the function returns the stderr output and an error.
func (r *FilesystemHoneytokenReconciler) executeCommandInContainer(ctx context.Context, pod corev1.Pod, containerName string, cmd []string) (string, error) {
	req := r.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command:   cmd,
			Container: containerName,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(&r.Config, "POST", req.URL())
	if err != nil {
		return "", err
	}

	// Create new buffers for the output
	var stdout, stderr bytes.Buffer

	// Execute the command
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return stderr.String(), err
	}

	return stdout.String(), nil
}
