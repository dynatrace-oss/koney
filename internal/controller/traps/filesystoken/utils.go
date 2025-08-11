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
	"encoding/json"

	kivev1 "github.com/San7o/kivebpf/api/v1"
	slimv1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	ciliumiov1alpha1 "github.com/cilium/tetragon/pkg/k8s/apis/cilium.io/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller/constants"
	"github.com/dynatrace-oss/koney/internal/controller/matching"
	"github.com/dynatrace-oss/koney/internal/controller/utils"
)

// GenerateTetragonTracingPolicyName generates the name of a Tetragon tracing policy based on the trap.
func GenerateTetragonTracingPolicyName(trap v1alpha1.Trap) (string, error) {
	trapJSON, err := json.Marshal(trap)
	if err != nil {
		return "", err
	}

	return "koney-tracing-policy-" + utils.Hash(string(trapJSON)), nil
}

// Similar to GenerateTetragonTracingPolicyName but used for Kive
func GenerateKiveTracingPolicyName(trap v1alpha1.Trap) (string, error) {
	// What is irrelevant for the policy should not alter the name, so
	// that there are no duplicate policies with different names.
	trap.DecoyDeployment.Strategy = ""
	trap.FilesystemHoneytoken.FileContent = ""
	trap.FilesystemHoneytoken.ReadOnly = false
	return GenerateTetragonTracingPolicyName(trap)
}

// createSecret creates a secret in the same namespace as the resource with the given name and data.
// The function does nothing if the secret already exists.
func createSecret(c client.Client, ctx context.Context, namespace, secretName string, data map[string][]byte) error {
	// Check if the secret already exists
	secret := corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, &secret); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
	}

	// If the secret does not exist, its Name is empty, so we create it
	if secret.Name == "" {
		secret = corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Data: data,
		}

		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			return c.Create(ctx, &secret)
		})
	}

	return nil
}

// generateSecretName generates the name of a secret based on different
// fields of a trap, depending on the trap type.
func generateSecretName(trap v1alpha1.Trap) string {
	var suffix string
	switch trap.TrapType() {
	case v1alpha1.FilesystemHoneytokenTrap:
		// The hash is calculated over the trap's filePath and fileContent
		suffix = utils.Hash(trap.FilesystemHoneytoken.FilePath + ":" + trap.FilesystemHoneytoken.FileContent)
	case v1alpha1.HttpEndpointTrap:
		suffix = "" // TODO: Implement.
	case v1alpha1.HttpPayloadTrap:
		suffix = "" // TODO: Implement.
	default:
		suffix = ""
	}

	return "koney-secret-" + suffix
}

// generateVolumeName generates the name of a volume based on the filePath.
func generateVolumeName(filePath string) string {
	return "koney-volume-" + utils.Hash(filePath)
}

// generateTetragonTracingPolicy generates a Tetragon tracing policy for a filesystem honeytoken trap.
func generateTetragonTracingPolicy(deceptionPolicy *v1alpha1.DeceptionPolicy, trap v1alpha1.Trap, tracingPolicyName string) (*ciliumiov1alpha1.TracingPolicy, error) {
	/*
		The `security_file_permission` function is a common execution point for the execution of
		system calls related to filesystem access, such as read, write, etc.
		Instead of tracing all filesystem access, we can just trace this function.

		Since processes can also access files by mapping them directly into their virtual address space
		and it is difficult to trace such access, we also monitor the `security_mmap_file` function,
		that is used when mapping a file into the virtual address space of a process.

		Finally, some system calls can be used to indirectly modify a file by changing its size (e.g., `truncate`).
		To trace such access, we also monitor the `security_path_truncate` function.

		We do not hook the `security_path_truncate` because this results in BPF compilation errors on some tested systems.

		See also:
		- https://tetragon.io/docs/use-cases/filename-access/#hooks

		Copyright (c) Cilium, Tetragon
		Dynatrace has made any changes to this code
		This code snippet is supplied without warranty, and is available under the Apache 2.0 license
		- https://raw.githubusercontent.com/cilium/tetragon/main/examples/tracingpolicy/filename_monitoring.yaml
	*/
	tracingPolicy := &ciliumiov1alpha1.TracingPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: tracingPolicyName,
			Labels: map[string]string{
				constants.LabelKeyDeceptionPolicyRef: deceptionPolicy.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         deceptionPolicy.APIVersion,
					Kind:               deceptionPolicy.Kind,
					Name:               deceptionPolicy.Name,
					UID:                deceptionPolicy.UID,
					BlockOwnerDeletion: &[]bool{true}[0], // A pointer to a bool
					Controller:         &[]bool{true}[0],
				},
			},
		},
		Spec: ciliumiov1alpha1.TracingPolicySpec{
			PodSelector: &slimv1.LabelSelector{
				MatchLabels: map[string]string{},
			},
			ContainerSelector: &slimv1.LabelSelector{},
			KProbes: []ciliumiov1alpha1.KProbeSpec{
				{
					Call:    "security_file_permission", // The security_file_permission function is used to trace filesystem access
					Syscall: false,
					Return:  true,
					Args: []ciliumiov1alpha1.KProbeArg{
						{
							Index: 0,
							Type:  "file", // A Linux file struct is used to get the file path
						},
					},
					ReturnArg: &ciliumiov1alpha1.KProbeArg{
						Index: 0,
						Type:  "int", // The int return type is used to trace the return value of the function
					},
					ReturnArgAction: "Post", // The Post action is used to trace the return value of the function
					Selectors: []ciliumiov1alpha1.KProbeSelector{
						{
							MatchArgs: []ciliumiov1alpha1.ArgSelector{
								{
									Index:    0,
									Operator: "Equal", // The Equal operator is used to match the file path
									Values: []string{
										trap.FilesystemHoneytoken.FilePath,
									},
								},
							},
							MatchActions: []ciliumiov1alpha1.ActionSelector{
								{
									Action: "GetUrl",
									ArgUrl: buildTetragonWebhookUrl(),
								},
							},
						},
					},
				},
				{
					Call:    "security_mmap_file", // The security_mmap_file function is used to trace memory-mapped files
					Syscall: false,
					Return:  true,
					Args: []ciliumiov1alpha1.KProbeArg{
						{
							Index: 0,
							Type:  "file",
						},
					},
					ReturnArg: &ciliumiov1alpha1.KProbeArg{
						Index: 0,
						Type:  "int",
					},
					ReturnArgAction: "Post",
					Selectors: []ciliumiov1alpha1.KProbeSelector{
						{
							MatchArgs: []ciliumiov1alpha1.ArgSelector{
								{
									Index:    0,
									Operator: "Equal",
									Values: []string{
										trap.FilesystemHoneytoken.FilePath,
									},
								},
							},
							MatchActions: []ciliumiov1alpha1.ActionSelector{
								{
									Action: "GetUrl",
									ArgUrl: buildTetragonWebhookUrl(),
								},
							},
						},
					},
				},
			},
		},
	}

	// Add the labels from the trap's MatchResources to the PodSelector
	for _, resourceFilter := range trap.MatchResources.Any {
		for key, value := range resourceFilter.Selector.MatchLabels {
			tracingPolicy.Spec.PodSelector.MatchLabels[key] = value
		}
	}

	for _, resourceFilter := range trap.MatchResources.Any {

		if matching.ContainerSelectorSelectsAll(resourceFilter.ContainerSelector) {
			// Empty the ContainerSelector, so that the TracingPolicy matches all containers
			if len(tracingPolicy.Spec.ContainerSelector.MatchExpressions) > 0 {
				tracingPolicy.Spec.ContainerSelector.MatchExpressions = []slimv1.LabelSelectorRequirement{}
			}

			// Break the loop, so that the ContainerSelector is not added to the TracingPolicy and we match all containers
			break
		} else {
			// Append the containerSelector to the ContainerSelector
			if len(tracingPolicy.Spec.ContainerSelector.MatchExpressions) == 0 {
				// Initialize the MatchExpressions
				tracingPolicy.Spec.ContainerSelector.MatchExpressions = []slimv1.LabelSelectorRequirement{}

				matchExpression := slimv1.LabelSelectorRequirement{
					Key:      "name",
					Operator: slimv1.LabelSelectorOpIn,
					Values:   []string{resourceFilter.ContainerSelector},
				}

				tracingPolicy.Spec.ContainerSelector.MatchExpressions = append(tracingPolicy.Spec.ContainerSelector.MatchExpressions, matchExpression)
			}

			// If the containerSelector is not already in the MatchExpressions, add it
			if !utils.Contains(tracingPolicy.Spec.ContainerSelector.MatchExpressions[0].Values, resourceFilter.ContainerSelector) {
				tracingPolicy.Spec.ContainerSelector.MatchExpressions[0].Values = append(tracingPolicy.Spec.ContainerSelector.MatchExpressions[0].Values, resourceFilter.ContainerSelector)
			}
		}
	}

	return tracingPolicy, nil
}

func buildTetragonWebhookUrl() string {
	return "http://koney-alert-forwarder-webhook." + utils.GetKoneyNamespace() + ".svc:8000/handlers/tetragon"
}

func buildKiveWebhookUrl() string {
	return "http://koney-alert-forwarder-webhook." + utils.GetKoneyNamespace() + ".svc:8000/handlers/kive"
}

// generateKiveTracingPolicy generates a Kive tracing policy for a filesystem honeytoken trap.
func generateKiveTracingPolicy(deceptionPolicy *v1alpha1.DeceptionPolicy, trap v1alpha1.Trap, tracingPolicyName string) (*kivev1.KivePolicy, error) {

	tracingPolicy := &kivev1.KivePolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KivePolicy",
			APIVersion: "kivebpf.san7o.github.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: tracingPolicyName,
			Labels: map[string]string{
				constants.LabelKeyDeceptionPolicyRef: deceptionPolicy.Name,
			},
			Finalizers: []string{kivev1.KivePolicyFinalizerName},
			Namespace:  utils.GetKoneyNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         deceptionPolicy.APIVersion,
					Kind:               deceptionPolicy.Kind,
					Name:               deceptionPolicy.Name,
					UID:                deceptionPolicy.UID,
					BlockOwnerDeletion: &[]bool{true}[0], // A pointer to a bool
					Controller:         &[]bool{true}[0],
				},
			},
		},
		Spec: kivev1.KivePolicySpec{},
	}

	kiveTrap := kivev1.KiveTrap{
		Path:     trap.FilesystemHoneytoken.FilePath,
		Callback: buildKiveWebhookUrl(),
		MatchAny: []kivev1.KiveTrapMatch{},
	}
	for _, resource := range trap.MatchResources.Any {

		kiveTrapMatches := []kivev1.KiveTrapMatch{}

		// If no namespaces are present, create a KiveTrapMatch anyway
		// with the other fields
		if len(resource.Namespaces) == 0 {
			kiveTrapMatch := kivev1.KiveTrapMatch{
				ContainerName: resource.ContainerSelector,
				MatchLabels:   map[string]string{},
			}

			for _, resourceFilter := range trap.MatchResources.Any {
				for key, value := range resourceFilter.Selector.MatchLabels {
					kiveTrapMatch.MatchLabels[key] = value
				}
			}

			kiveTrapMatches = append(kiveTrapMatches, kiveTrapMatch)

		} else {

			for _, namespace := range resource.Namespaces {

				kiveTrapMatch := kivev1.KiveTrapMatch{
					Namespace:     namespace,
					ContainerName: resource.ContainerSelector,
					MatchLabels:   map[string]string{},
				}

				for _, resourceFilter := range trap.MatchResources.Any {
					for key, value := range resourceFilter.Selector.MatchLabels {
						kiveTrapMatch.MatchLabels[key] = value
					}
				}

				kiveTrapMatches = append(kiveTrapMatches, kiveTrapMatch)
			}
		}

		kiveTrap.MatchAny = append(kiveTrap.MatchAny, kiveTrapMatches...)
	}

	kiveTraps := []kivev1.KiveTrap{kiveTrap}
	tracingPolicy.Spec.Traps = kiveTraps

	return tracingPolicy, nil
}
