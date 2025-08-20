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
	slimv1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller/matching"
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
				tracingPolicy := generateTetragonTracingPolicy(&deceptionPolicy, trap, "test-tracing-policy")
				Expect(tracingPolicy.Name).To(Equal("test-tracing-policy"))

				// Check the label selector
				for _, resourceFilter := range trap.MatchResources.Any {
					for key, value := range resourceFilter.Selector.MatchLabels {
						Expect(tracingPolicy.Spec.PodSelector.MatchLabels[key]).To(Equal(value))
					}
				}

				// Check the container selector
				for _, resourceFilter := range trap.MatchResources.Any {
					if matching.ContainerSelectorSelectsAll(resourceFilter.ResourceDescription.ContainerSelector) {
						Expect(tracingPolicy.Spec.ContainerSelector.MatchExpressions).To(BeEmpty())
					} else {
						Expect(tracingPolicy.Spec.ContainerSelector.MatchExpressions).To(HaveLen(1))
						Expect(tracingPolicy.Spec.ContainerSelector.MatchExpressions[0].Key).To(Equal("name"))
						Expect(tracingPolicy.Spec.ContainerSelector.MatchExpressions[0].Operator).To(Equal(slimv1.LabelSelectorOpIn))
						Expect(tracingPolicy.Spec.ContainerSelector.MatchExpressions[0].Values).To(ConsistOf(resourceFilter.ContainerSelector))
					}
				}
			}
		})
	})

})
