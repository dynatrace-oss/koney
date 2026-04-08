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

package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var testTraps []Trap

// initializeTestTraps initializes the traps with all possible permutations of values
func initializeTestTraps() {
	var (
		deploymentStrategyValues = []string{
			"volumeMount",
			"containerExec",
			"kyvernoPolicy",
		}

		trapTypeValues = []string{
			"filesystemHoneytoken",
		}

		sampleSelector = metav1.LabelSelector{
			MatchLabels: map[string]string{"deceptionpolicies.research.dynatrace.com/label": "true"},
		}

		matchOnlyNamespace = []ResourceFilter{
			{ResourceDescription: ResourceDescription{Namespaces: []string{"koney"}}},
		}
		matchOnlySelector = []ResourceFilter{
			{ResourceDescription: ResourceDescription{Selector: &sampleSelector}},
		}
		matchBothNamespaceAndSelector = []ResourceFilter{
			{ResourceDescription: ResourceDescription{Namespaces: []string{"koney"}, Selector: &sampleSelector}},
		}

		matchResourcesValues = []MatchResources{
			{Any: matchOnlyNamespace},
			{Any: matchOnlySelector},
			{Any: matchBothNamespaceAndSelector},
		}
	)

	for _, deploymentStrategy := range deploymentStrategyValues {
		for _, matchResources := range matchResourcesValues {
			for _, trapType := range trapTypeValues {
				switch trapType {
				case "filesystemHoneytoken":
					trap := Trap{
						FilesystemHoneytoken: FilesystemHoneytoken{
							FilePath:    "/run/secrets/koney/service_token",
							FileContent: "{\"service_token\":\"🐢\"}",
							ReadOnly:    true,
						},
						DecoyDeployment: DecoyDeployment{
							Strategy: deploymentStrategy,
						},
						MatchResources: matchResources,
					}
					testTraps = append(testTraps, trap)
				case "httpEndpoint":
					// TODO: Implement.
				case "httpPayload":
					// TODO: Implement.
				}
			}
		}
	}
}

var _ = Describe("TrapType", func() {
	Context("when getting the trap type", func() {
		It("should return the correct type", func() {
			for _, trap := range testTraps {
				switch trap.TrapType() {
				case FilesystemHoneytokenTrap:
					Expect(trap.FilesystemHoneytoken).NotTo(BeNil())
				case HttpEndpointTrap:
					Expect(trap.HttpEndpoint).NotTo(BeNil())
				case HttpPayloadTrap:
					Expect(trap.HttpPayload).NotTo(BeNil())
				default:
					Expect(trap.TrapType()).To(Equal(UnknownTrap))
				}
			}
		})
	})
})

var _ = Describe("IsValid", func() {
	Context("when checking a valid trap", func() {
		It("should return no error", func() {
			for _, trap := range testTraps {
				err := trap.IsValid()
				Expect(err).ShouldNot(HaveOccurred())
			}
		})
	})

	Context("when checking a trap with an empty MatchResources", func() {
		It("should return error", func() {
			for _, trap := range testTraps {
				trap.MatchResources = MatchResources{}
				err := trap.IsValid()
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("is nil"))
			}
		})
	})

	Context("when checking a trap with both Namespaces and Selector nil", func() {
		It("should return error", func() {
			for _, trap := range testTraps {
				trap.MatchResources = MatchResources{
					Any: []ResourceFilter{
						{ResourceDescription: ResourceDescription{Namespaces: nil, Selector: nil}},
					},
				}
				err := trap.IsValid()
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("are nil"))
			}
		})
	})

	Context("when checking a trap with both Namespaces and Selector empty", func() {
		It("should return error", func() {
			for _, trap := range testTraps {
				trap.MatchResources = MatchResources{
					Any: []ResourceFilter{
						{
							ResourceDescription: ResourceDescription{
								Selector:   &metav1.LabelSelector{},
								Namespaces: []string{},
							},
						},
					},
				}
				err := trap.IsValid()
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("are empty"))
			}
		})
	})

	Context("when checking a filesystem honeytoken trap with a non-absolute file path", func() {
		It("should return error", func() {
			for _, trap := range testTraps {
				// Remove the first character to make the path relative
				trap.FilesystemHoneytoken.FilePath = trap.FilesystemHoneytoken.FilePath[1:]
				err := trap.IsValid()
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("is not absolute"))
			}
		})
	})

	Context("when checking a trap with captor strategy 'none'", func() {
		It("should be valid", func() {
			for _, trap := range testTraps {
				trap.CaptorDeployment = CaptorDeployment{Strategy: "none"}
				err := trap.IsValid()
				Expect(err).ShouldNot(HaveOccurred())
			}
		})
	})
})
