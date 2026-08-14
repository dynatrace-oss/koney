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

	slimv1 "github.com/cilium/tetragon/pkg/k8s/slim/k8s/apis/meta/v1"
	"github.com/go-logr/logr/funcr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller/constants"
	"github.com/dynatrace-oss/koney/internal/controller/matching"
	trapsapi "github.com/dynatrace-oss/koney/internal/controller/traps/api"
)

var (
	containerSelectorValues = []string{
		"name",
		"glob:namewithwildcard*",
		"glob:namewithwildcard?",
		"regex:.*",
	}

	labelSelectorValues = metav1.LabelSelector{
		MatchLabels: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	helpersTraps []v1alpha1.Trap
)

// initializeTestTraps initializes the traps with all possible permutations of values to test the annotations.
// These traps are used to test the GenerateTetragonTracingPolicy function, therefore,
// we vary the MatchResources field, which is what is used when generating the policy.
func initializeTestTraps() {
	for _, containerSelector := range containerSelectorValues {
		trap := v1alpha1.Trap{
			FilesystemHoneytoken: v1alpha1.FilesystemHoneytoken{
				FilePath:    "/path/to/file",
				FileContent: "someverysecrettoken", // This is not included in the Tetragon TracingPolicy
			},
			DecoyDeployment: v1alpha1.DecoyDeployment{}, // This is not included in the Tetragon TracingPolicy
			CaptorDeployment: v1alpha1.CaptorDeployment{
				Strategy: "tetragon", // This is not included in the Tetragon TracingPolicy
			},
			MatchResources: v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector:          &labelSelectorValues,
							ContainerSelector: containerSelector,
						},
					},
				},
			},
		}
		helpersTraps = append(helpersTraps, trap)
	}
}

var _ = Describe("generateTetragonTracingPolicy", func() {
	Context("With a trap", func() {
		It("should generate a Tetragon TracingPolicy", func() {
			for _, trap := range helpersTraps {
				deceptionPolicy := v1alpha1.DeceptionPolicy{
					Spec: v1alpha1.DeceptionPolicySpec{
						Traps: []v1alpha1.Trap{trap},
					},
				}
				tracingPolicy := generateTetragonTracingPolicy(context.Background(), &deceptionPolicy, trap, "test-tracing-policy")
				Expect(tracingPolicy.Name).To(Equal("test-tracing-policy"))

				// Check the label selector
				for _, resourceFilter := range trap.MatchResources.Any {
					for key, value := range resourceFilter.Selector.MatchLabels {
						Expect(tracingPolicy.Spec.PodSelector.MatchLabels[key]).To(Equal(value))
					}
				}

				// Check the container selector
				for _, resourceFilter := range trap.MatchResources.Any {
					if matching.ContainerSelectorSelectsAll(resourceFilter.ContainerSelector) {
						// Case 1: selects all → empty ContainerSelector, no annotation
						Expect(tracingPolicy.Spec.ContainerSelector.MatchExpressions).To(BeEmpty())
						Expect(tracingPolicy.Annotations).NotTo(HaveKey(constants.AnnotationKeyContainerSelectors))
					} else if matching.ContainerSelectorNeedsClientFiltering(resourceFilter.ContainerSelector) {
						// Case 2: wildcard pattern → empty ContainerSelector, annotation with all selectors
						Expect(tracingPolicy.Spec.ContainerSelector.MatchExpressions).To(BeEmpty())
						Expect(tracingPolicy.Annotations).To(HaveKey(constants.AnnotationKeyContainerSelectors))
						var selectors []string
						Expect(json.Unmarshal([]byte(tracingPolicy.Annotations[constants.AnnotationKeyContainerSelectors]), &selectors)).To(Succeed())
						Expect(selectors).To(ContainElement(resourceFilter.ContainerSelector))
					} else {
						// Case 3: exact name → ContainerSelector with MatchExpressions, no annotation
						Expect(tracingPolicy.Spec.ContainerSelector.MatchExpressions).To(HaveLen(1))
						Expect(tracingPolicy.Spec.ContainerSelector.MatchExpressions[0].Key).To(Equal("name"))
						Expect(tracingPolicy.Spec.ContainerSelector.MatchExpressions[0].Operator).To(Equal(slimv1.LabelSelectorOpIn))
						Expect(tracingPolicy.Spec.ContainerSelector.MatchExpressions[0].Values).To(ConsistOf(resourceFilter.ContainerSelector))
						Expect(tracingPolicy.Annotations).NotTo(HaveKey(constants.AnnotationKeyContainerSelectors))
					}
				}
			}
		})
	})

})

var _ = Describe("DeployCaptor", func() {
	Context("with captor strategy 'none'", func() {
		It("should return success without deploying any resources", func() {
			trap := v1alpha1.Trap{
				FilesystemHoneytoken: v1alpha1.FilesystemHoneytoken{
					FilePath: "/run/secrets/koney/service_token",
				},
				CaptorDeployment: v1alpha1.CaptorDeployment{
					Strategy: "none",
				},
				MatchResources: v1alpha1.MatchResources{
					Any: []v1alpha1.ResourceFilter{
						{ResourceDescription: v1alpha1.ResourceDescription{
							Namespaces: []string{"koney"},
						}},
					},
				},
			}
			deceptionPolicy := &v1alpha1.DeceptionPolicy{}
			reconciler := FilesystemHoneytokenReconciler{}
			result := reconciler.DeployCaptor(context.Background(), deceptionPolicy, trap)
			Expect(result.Errors).ToNot(HaveOccurred())
			Expect(result.MissingTetragon).To(BeFalse())
		})
	})
})

var _ = Describe("Policy generation with a trap that uses matchExpressions", func() {
	expressionsTrap := v1alpha1.Trap{
		FilesystemHoneytoken: v1alpha1.FilesystemHoneytoken{
			FilePath:    "/path/to/file",
			FileContent: "someverysecrettoken",
		},
		MatchResources: v1alpha1.MatchResources{
			Any: []v1alpha1.ResourceFilter{
				{
					ResourceDescription: v1alpha1.ResourceDescription{
						Namespaces: []string{"koney"},
						Selector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"backend"},
								},
							},
						},
					},
				},
			},
		},
	}

	deceptionPolicy := v1alpha1.DeceptionPolicy{
		Spec: v1alpha1.DeceptionPolicySpec{
			Traps: []v1alpha1.Trap{expressionsTrap},
		},
	}

	It("should copy the expressions into the Tetragon PodSelector", func() {
		tracingPolicy := generateTetragonTracingPolicy(context.Background(), &deceptionPolicy, expressionsTrap, "test-tracing-policy")
		Expect(tracingPolicy.Spec.PodSelector.MatchExpressions).To(HaveLen(1))
		Expect(tracingPolicy.Spec.PodSelector.MatchExpressions[0].Key).To(Equal("app"))
		Expect(tracingPolicy.Spec.PodSelector.MatchExpressions[0].Operator).To(Equal(slimv1.LabelSelectorOpIn))
		Expect(tracingPolicy.Spec.PodSelector.MatchExpressions[0].Values).To(ConsistOf("backend"))
	})

	It("should skip the resource filter in the Kive policy and log a warning", func() {
		messages := []string{}
		logger := funcr.New(func(prefix, args string) {
			messages = append(messages, args)
		}, funcr.Options{})
		ctx := k8slog.IntoContext(context.Background(), logger)

		kivePolicy := generateKivePolicy(ctx, &deceptionPolicy, expressionsTrap, "test-kive-policy")
		Expect(kivePolicy.Spec.Traps[0].MatchAny).To(BeEmpty())
		Expect(messages).To(ContainElement(ContainSubstring("matchExpressions")))
	})

	It("should keep resource filters that do not use expressions, without taking over their labels", func() {
		mixedTrap := expressionsTrap
		mixedTrap.MatchResources = v1alpha1.MatchResources{
			Any: []v1alpha1.ResourceFilter{
				{
					ResourceDescription: v1alpha1.ResourceDescription{
						Namespaces: []string{"other"},
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "frontend"},
						},
					},
				},
				{
					ResourceDescription: v1alpha1.ResourceDescription{
						Namespaces: []string{"koney"},
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "backend", "tier": "db"},
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "env",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"prod"},
								},
							},
						},
					},
				},
			},
		}

		kivePolicy := generateKivePolicy(context.Background(), &deceptionPolicy, mixedTrap, "test-kive-policy")
		Expect(kivePolicy.Spec.Traps[0].MatchAny).To(HaveLen(1))
		Expect(kivePolicy.Spec.Traps[0].MatchAny[0].Namespace).To(Equal("other"))
		Expect(kivePolicy.Spec.Traps[0].MatchAny[0].MatchLabels).To(Equal(map[string]string{"app": "frontend"}))
	})

	It("should warn that the captor does not watch all selected resources", func() {
		Expect(trapUsesMatchExpressions(expressionsTrap)).To(BeTrue())

		result := trapsapi.CaptorDeploymentResult{Trap: &expressionsTrap, UnsupportedSelectors: true}
		Expect(result.ImpliesSuccess()).To(BeFalse())
		Expect(result.ImpliesFailure()).To(BeTrue())
	})
})

var _ = Describe("Policy generation with multiple resource filters", func() {
	deceptionPolicy := v1alpha1.DeceptionPolicy{}

	trapWithTwoFilters := v1alpha1.Trap{
		FilesystemHoneytoken: v1alpha1.FilesystemHoneytoken{
			FilePath:    "/path/to/file",
			FileContent: "someverysecrettoken",
		},
		MatchResources: v1alpha1.MatchResources{
			Any: []v1alpha1.ResourceFilter{
				{
					ResourceDescription: v1alpha1.ResourceDescription{
						Namespaces: []string{"ns-a"},
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "frontend"},
						},
					},
				},
				{
					ResourceDescription: v1alpha1.ResourceDescription{
						Namespaces: []string{"ns-b"},
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "backend", "tier": "db"},
						},
					},
				},
			},
		},
	}

	It("should not mix the labels of different resource filters in the Kive policy", func() {
		kivePolicy := generateKivePolicy(context.Background(), &deceptionPolicy, trapWithTwoFilters, "test-kive-policy")

		Expect(kivePolicy.Spec.Traps[0].MatchAny).To(HaveLen(2))
		Expect(kivePolicy.Spec.Traps[0].MatchAny[0].Namespace).To(Equal("ns-a"))
		Expect(kivePolicy.Spec.Traps[0].MatchAny[0].MatchLabels).To(Equal(map[string]string{"app": "frontend"}))
		Expect(kivePolicy.Spec.Traps[0].MatchAny[1].Namespace).To(Equal("ns-b"))
		Expect(kivePolicy.Spec.Traps[0].MatchAny[1].MatchLabels).To(Equal(map[string]string{"app": "backend", "tier": "db"}))
	})

	It("should warn that the merged Tetragon podSelector matches fewer pods than the trap selects", func() {
		trapWithTwoExpressions := trapWithTwoFilters
		trapWithTwoExpressions.MatchResources = v1alpha1.MatchResources{
			Any: []v1alpha1.ResourceFilter{
				{
					ResourceDescription: v1alpha1.ResourceDescription{
						Selector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{Key: "env", Operator: metav1.LabelSelectorOpIn, Values: []string{"prod"}},
							},
						},
					},
				},
				{
					ResourceDescription: v1alpha1.ResourceDescription{
						Selector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{Key: "env", Operator: metav1.LabelSelectorOpIn, Values: []string{"dev"}},
							},
						},
					},
				},
			},
		}

		messages := []string{}
		logger := funcr.New(func(prefix, args string) {
			messages = append(messages, args)
		}, funcr.Options{})
		ctx := k8slog.IntoContext(context.Background(), logger)

		tracingPolicy := generateTetragonTracingPolicy(ctx, &deceptionPolicy, trapWithTwoExpressions, "test-tracing-policy")

		// Both expressions end up in the single podSelector, where they are combined with a logical AND
		Expect(tracingPolicy.Spec.PodSelector.MatchExpressions).To(HaveLen(2))
		Expect(messages).To(ContainElement(ContainSubstring("logical AND")))
	})
})

var _ = Describe("Policy generation with a namespace-only trap", func() {
	Context("With a trap that matches resources by namespace and has no label selector", func() {
		namespaceOnlyTrap := v1alpha1.Trap{
			FilesystemHoneytoken: v1alpha1.FilesystemHoneytoken{
				FilePath:    "/path/to/file",
				FileContent: "someverysecrettoken",
			},
			MatchResources: v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Namespaces: []string{"koney"},
						},
					},
				},
			},
		}

		It("should generate a Tetragon TracingPolicy without a PodSelector label", func() {
			deceptionPolicy := v1alpha1.DeceptionPolicy{
				Spec: v1alpha1.DeceptionPolicySpec{
					Traps: []v1alpha1.Trap{namespaceOnlyTrap},
				},
			}
			tracingPolicy := generateTetragonTracingPolicy(context.Background(), &deceptionPolicy, namespaceOnlyTrap, "test-tracing-policy")
			Expect(tracingPolicy.Name).To(Equal("test-tracing-policy"))
			Expect(tracingPolicy.Spec.PodSelector.MatchLabels).To(BeEmpty())
		})

		It("should generate a KivePolicy without a match label", func() {
			deceptionPolicy := v1alpha1.DeceptionPolicy{
				Spec: v1alpha1.DeceptionPolicySpec{
					Traps: []v1alpha1.Trap{namespaceOnlyTrap},
				},
			}
			kivePolicy := generateKivePolicy(context.Background(), &deceptionPolicy, namespaceOnlyTrap, "test-kive-policy")
			Expect(kivePolicy.Name).To(Equal("test-kive-policy"))
			Expect(kivePolicy.Spec.Traps[0].MatchAny).ToNot(BeEmpty())
			for _, trapMatch := range kivePolicy.Spec.Traps[0].MatchAny {
				Expect(trapMatch.Namespace).To(Equal("koney"))
				Expect(trapMatch.MatchLabels).To(BeEmpty())
			}
		})
	})
})
