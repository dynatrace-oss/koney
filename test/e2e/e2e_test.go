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

package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dynatrace-oss/koney/api/v1alpha1"

	"github.com/dynatrace-oss/koney/internal/controller/constants"
	"github.com/dynatrace-oss/koney/internal/controller/utils"
	testutils "github.com/dynatrace-oss/koney/test/utils"
)

const (
	imageTagPrefix = "e2e-tests-"

	testNamespace = "koney-tests"
	testCrdName   = "deceptionpolicies.research.dynatrace.com"

	manifestsDir = "test/e2e/manifests"

	nameOfTestDeployment  = "koney-test-deployment"
	labelOfTestDeployment = "app=koney-test-pod"
	yamlOfTestDeployment  = manifestsDir + "/deployments/test_deployment_two_containers_nginx_alpine.yaml"

	nameOfExtraTestPod = "koney-extra-test-pod"
	yamlOfExtraTestPod = manifestsDir + "/pods/test_pod_extra.yaml"

	nameOfDeceptionPolicy              = "koney-test-deceptionpolicy"
	yamlOfOneFilesystokenContainerExec = manifestsDir + "/deceptionpolicies/test_trap_filesystoken_container_exec.yaml"
	yamlOfTwoFilesystokenContainerExec = manifestsDir + "/deceptionpolicies/test_trap_two_filesystokens.yaml"
	yamlOfFilesystokenNoMutateExisting = manifestsDir + "/deceptionpolicies/test_trap_filesystoken_no_mutate_existing.yaml"
	yamlOfFilesystokenVolumeMount      = manifestsDir + "/deceptionpolicies/test_trap_filesystoken_volume_mount.yaml"

	statusRunning = "Running"
)

var (
	projectDir, _ = testutils.GetProjectDir()

	// lastModificationTime is the time when the DeceptionPolicy was last created, updated or deleted by us
	lastModificationTime time.Time

	// allFilesystemHoneytokenPaths is a list of all paths created during tests, which should be removed at the end
	allFilesystemHoneytokenPaths []string

	// controllerPodName is the actual name of the Koney controller pod
	controllerPodName string

	// testPodName is the actual name of the test pod, which was created by the test deployment
	testPodName string

	// controllerContainersPolicyShouldMatch is the only list of containers that all policies should match
	containersPolicyShouldMatch = []string{"nginx"}

	// managerNamespace is the namespace where the controller-manager is deployed
	managerNamespace = utils.GetKoneyNamespace()
)

//nolint:dupl
var _ = Describe("Koney Operator", Ordered, func() {
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", managerNamespace)
		_, _ = testutils.Run(cmd)

		By("creating test namespace")
		cmd = exec.Command("kubectl", "create", "ns", testNamespace)
		_, _ = testutils.Run(cmd)
	})

	AfterAll(func() {
		By("removing test namespace")
		cmd := exec.Command("kubectl", "delete", "ns", testNamespace)
		_, _ = testutils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", managerNamespace)
		_, _ = testutils.Run(cmd)
	})

	When("deploying the controller-manager", func() {
		It("should deploy the controller-manager", func() {
			var err error

			// Add timestamp to the image name to avoid conflicts
			var imageTag = imageTagPrefix + fmt.Sprintf("%d", time.Now().Unix())

			By("building the manager(Operator) image")
			cmd := exec.Command("make", "docker-build", fmt.Sprintf("VERSION=%s", imageTag))
			_, err = testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("loading the the manager(Operator) image on aws")
			cmd = exec.Command("make", "docker-push", fmt.Sprintf("VERSION=%s", imageTag))
			_, err = testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("deploying the controller-manager")
			cmd = exec.Command("make", "deploy", fmt.Sprintf("VERSION=%s", imageTag))
			_, err = testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func() error {
				// Get pod name
				cmd = exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", managerNamespace,
				)

				podOutput, err := testutils.Run(cmd)
				ExpectWithOffset(testutils.Offset, err).NotTo(HaveOccurred())
				podNames := testutils.GetNonEmptyLines(string(podOutput))
				if len(podNames) != 1 {
					return fmt.Errorf("expect 1 controller pods running, but got %d", len(podNames))
				}
				controllerPodName = podNames[0]
				ExpectWithOffset(testutils.Offset, controllerPodName).Should(ContainSubstring("controller-manager"))

				// Validate pod status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", managerNamespace,
				)
				status, err := testutils.Run(cmd)
				ExpectWithOffset(testutils.Offset, err).NotTo(HaveOccurred())
				if string(status) != statusRunning {
					return fmt.Errorf("controller pod in %s status", status)
				}

				// Wait for readiness
				cmd = exec.Command("kubectl", "wait",
					"--for=condition=Ready", "pod", "-l", "control-plane=controller-manager",
					"-n", managerNamespace)
				_, err = testutils.Run(cmd)
				ExpectWithOffset(testutils.Offset, err).NotTo(HaveOccurred())

				return nil
			}
			Eventually(verifyControllerUp, time.Minute, time.Second).Should(Succeed())
		})
	})

	When("creating a test pod and a DeceptionPolicy CR", func() {
		It("should create a honeytoken in the test pod", func() {
			By("creating a test pod")
			cmd := exec.Command("kubectl", "apply", "-n", testNamespace,
				"-f", filepath.Join(projectDir, yamlOfTestDeployment))
			_, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the test pod is running as expected")
			Expect(waitDeploymentReady(testNamespace, nameOfTestDeployment)).To(Succeed())
			Eventually(func() error {
				return verifyTestPodRunningByLabel(testNamespace, labelOfTestDeployment, &testPodName)
			}, time.Minute, time.Second).Should(Succeed())

			// We deploy a DeceptionPolicy CR that creates a honeytoken in the test pod
			By("creating a Koney DeceptionPolicy CR")
			lastModificationTime = time.Now().UTC()
			cmd = exec.Command("kubectl", "apply", "-n", testNamespace,
				"-f", filepath.Join(projectDir, yamlOfOneFilesystokenContainerExec))
			_, err = testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Every time that a DeceptionPolicy is created,
			// we need to wait for the pod to be ready (the deployment may be updated)
			By("validating that the test pod is running as expected")
			Expect(waitDeploymentReady(testNamespace, nameOfTestDeployment)).To(Succeed())
			Eventually(func() error {
				return verifyTestPodRunningByLabel(testNamespace, labelOfTestDeployment, &testPodName)
			}, time.Minute, time.Second).Should(Succeed())

			// Get the DeceptionPolicy CR to get the expected value of the annotation
			var deceptionPolicy v1alpha1.DeceptionPolicy
			cmd = exec.Command("kubectl", "get", testCrdName, nameOfDeceptionPolicy, "-o", "json", "-n", testNamespace)
			deceptionPolicyJSON, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			err = json.Unmarshal(deceptionPolicyJSON, &deceptionPolicy)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is present in the test pod")
			Eventually(func() error {
				return verifyAnnotationPresentInPod(testNamespace, testPodName)
			}, time.Minute, time.Second).Should(Succeed())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is accurate in the test pod")
			Eventually(func() error {
				return verifyAnnotationIsAccurate(testNamespace, "pod", testPodName,
					nameOfDeceptionPolicy, deceptionPolicy.Spec.Traps, &allFilesystemHoneytokenPaths)
			}, time.Minute, time.Second).Should(Succeed())

			updateObservedFilePaths(deceptionPolicy.Spec.Traps, &allFilesystemHoneytokenPaths)

			By("validating that the honeytoken is created in the test pod and has the expected content")
			for _, trap := range deceptionPolicy.Spec.Traps {
				err := verifyHoneytokenAndAwaitAlertTetragon(trap, lastModificationTime,
					testNamespace, testPodName, containersPolicyShouldMatch)
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("validating that the status conditions of the DeceptionPolicy are accurate")
			Eventually(func() error {
				return verifyStatusConditions(testNamespace, testCrdName, nameOfDeceptionPolicy, true, true)
			}, time.Minute, time.Second).Should(Succeed())
		})
	})

	When("updating the DeceptionPolicy CR", func() {
		It("should update the honeytoken in the test pod", func() {
			By("updating the DeceptionPolicy CR")
			lastModificationTime = time.Now().UTC()
			cmd := exec.Command("kubectl", "apply", "-n", testNamespace,
				"-f", filepath.Join(projectDir, yamlOfTwoFilesystokenContainerExec))
			_, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the test pod is running as expected")
			Expect(waitDeploymentReady(testNamespace, nameOfTestDeployment)).To(Succeed())
			Eventually(func() error {
				return verifyTestPodRunningByLabel(testNamespace, labelOfTestDeployment, &testPodName)
			}, time.Minute, time.Second).Should(Succeed())

			// Get the DeceptionPolicy CR to get the expected value of the annotation
			var deceptionPolicy v1alpha1.DeceptionPolicy
			cmd = exec.Command("kubectl", "get", testCrdName, nameOfDeceptionPolicy, "-o", "json", "-n", testNamespace)
			deceptionPolicyJSON, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			err = json.Unmarshal(deceptionPolicyJSON, &deceptionPolicy)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is present in the test pod")
			Eventually(func() error {
				return verifyAnnotationPresentInPod(testNamespace, testPodName)
			}, time.Minute, time.Second).Should(Succeed())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is accurate in the test pod")
			Eventually(func() error {
				return verifyAnnotationIsAccurate(testNamespace, "pod", testPodName,
					nameOfDeceptionPolicy, deceptionPolicy.Spec.Traps, &allFilesystemHoneytokenPaths)
			}, time.Minute, time.Second).Should(Succeed())

			updateObservedFilePaths(deceptionPolicy.Spec.Traps, &allFilesystemHoneytokenPaths)

			By("validating that the honeytoken is updated in the test pod and has the expected content")
			for _, trap := range deceptionPolicy.Spec.Traps {
				err := verifyHoneytokenAndAwaitAlertTetragon(trap, lastModificationTime,
					testNamespace, testPodName, containersPolicyShouldMatch)
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("validating that the status conditions of the DeceptionPolicy are accurate")
			Eventually(func() error {
				return verifyStatusConditions(testNamespace, testCrdName, nameOfDeceptionPolicy, true, true)
			}, time.Minute, time.Second).Should(Succeed())
		})
	})

	When("creating a new pod in the cluster after the DeceptionPolicy was already deployed", func() {
		It("should update the honeytoken in the extra test pod", func() {
			By("creating an extra test pod")
			cmd := exec.Command("kubectl", "apply", "-n", testNamespace,
				"-f", filepath.Join(projectDir, yamlOfExtraTestPod))
			_, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the extra test pod is running as expected")
			Eventually(func() error {
				return verifyTestPodRunningByName(testNamespace, nameOfExtraTestPod)
			}, time.Minute, time.Second).Should(Succeed())

			// Get the DeceptionPolicy CR to get the expected value of the annotation
			var deceptionPolicy v1alpha1.DeceptionPolicy
			cmd = exec.Command("kubectl", "get", testCrdName, nameOfDeceptionPolicy, "-o", "json", "-n", testNamespace)
			deceptionPolicyJSON, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			err = json.Unmarshal(deceptionPolicyJSON, &deceptionPolicy)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is present in the extra test pod")
			Eventually(func() error {
				return verifyAnnotationPresentInPod(testNamespace, nameOfExtraTestPod)
			}, time.Minute, time.Second).Should(Succeed())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is accurate in the extra test pod")
			Eventually(func() error {
				return verifyAnnotationIsAccurate(testNamespace, "pod", nameOfExtraTestPod,
					nameOfDeceptionPolicy, deceptionPolicy.Spec.Traps, &allFilesystemHoneytokenPaths)
			}, time.Minute, time.Second).Should(Succeed())

			updateObservedFilePaths(deceptionPolicy.Spec.Traps, &allFilesystemHoneytokenPaths)

			By("validating that the honeytoken is created in the extra test pod and has the expected content")
			for _, trap := range deceptionPolicy.Spec.Traps {
				// we re-use the lastModificationTime from the previous test because no probes should have needed an update
				err := verifyHoneytokenAndAwaitAlertTetragon(trap, lastModificationTime,
					testNamespace, nameOfExtraTestPod, containersPolicyShouldMatch)
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("validating that the status conditions of the DeceptionPolicy are accurate")
			Eventually(func() error {
				return verifyStatusConditions(testNamespace, testCrdName, nameOfDeceptionPolicy, true, true)
			}, time.Minute, time.Second).Should(Succeed())
		})
	})

	When("Reverting the changes to the DeceptionPolicy CR", func() {
		It("should revert the honeytoken in the test pod", func() {
			By("reverting the DeceptionPolicy CR")
			lastModificationTime = time.Now().UTC()
			cmd := exec.Command("kubectl", "apply", "-n", testNamespace,
				"-f", filepath.Join(projectDir, yamlOfOneFilesystokenContainerExec))
			_, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the test pod is running as expected")
			Expect(waitDeploymentReady(testNamespace, nameOfTestDeployment)).To(Succeed())
			Eventually(func() error {
				return verifyTestPodRunningByLabel(testNamespace, labelOfTestDeployment, &testPodName)
			}, time.Minute, time.Second).Should(Succeed())

			// Get the DeceptionPolicy CR to get the expected value of the annotation
			var deceptionPolicy v1alpha1.DeceptionPolicy
			cmd = exec.Command("kubectl", "get", testCrdName, nameOfDeceptionPolicy, "-o", "json", "-n", testNamespace)
			deceptionPolicyJSON, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			err = json.Unmarshal(deceptionPolicyJSON, &deceptionPolicy)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is present in the test pod")
			Eventually(func() error {
				return verifyAnnotationPresentInPod(testNamespace, testPodName)
			}, time.Minute, time.Second).Should(Succeed())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is accurate in the test pod")
			Eventually(func() error {
				return verifyAnnotationIsAccurate(testNamespace, "pod", testPodName,
					nameOfDeceptionPolicy, deceptionPolicy.Spec.Traps, &allFilesystemHoneytokenPaths)
			}, time.Minute, time.Second).Should(Succeed())

			updateObservedFilePaths(deceptionPolicy.Spec.Traps, &allFilesystemHoneytokenPaths)

			By("validating that the honeytoken is updated in the test pod and has the expected content")
			for _, trap := range deceptionPolicy.Spec.Traps {
				err := verifyHoneytokenAndAwaitAlertTetragon(trap, lastModificationTime,
					testNamespace, testPodName, containersPolicyShouldMatch)
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("validating that the status conditions of the DeceptionPolicy are accurate")
			Eventually(func() error {
				return verifyStatusConditions(testNamespace, testCrdName, nameOfDeceptionPolicy, true, true)
			}, time.Minute, time.Second).Should(Succeed())
		})
	})

	When("updating the DeceptionPolicy CR with a trap deployed using the volumeMount strategy", func() {
		It("should update the honeytoken in the test pod", func() {
			By("updating the DeceptionPolicy CR")
			lastModificationTime = time.Now().UTC()
			cmd := exec.Command("kubectl", "apply", "-n", testNamespace,
				"-f", filepath.Join(projectDir, yamlOfFilesystokenVolumeMount))
			_, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Restart deployment and wait
			cmd = exec.Command("kubectl", "rollout", "restart", "deployment", nameOfTestDeployment, "-n", testNamespace)
			_, err = testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			cmd = exec.Command("kubectl", "rollout", "status", "deployment",
				nameOfTestDeployment, "-n", testNamespace, "--timeout=1m")
			_, err = testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Get the DeceptionPolicy CR to get the expected value of the annotation
			var deceptionPolicy v1alpha1.DeceptionPolicy
			cmd = exec.Command("kubectl", "get", testCrdName, nameOfDeceptionPolicy, "-o", "json", "-n", testNamespace)
			deceptionPolicyJSON, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			err = json.Unmarshal(deceptionPolicyJSON, &deceptionPolicy)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the test pod is running as expected")
			Expect(waitDeploymentReady(testNamespace, nameOfTestDeployment)).To(Succeed())
			Eventually(func() error {
				return verifyTestPodRunningByLabel(testNamespace, labelOfTestDeployment, &testPodName)
			}, time.Minute, time.Second).Should(Succeed())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is present in the test deployment")
			Eventually(func() error {
				return verifyAnnotationPresentInDeployment(testNamespace, nameOfTestDeployment)
			}, time.Minute, time.Second).Should(Succeed())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is accurate in the test deployment")
			Eventually(func() error {
				return verifyAnnotationIsAccurate(testNamespace, "deployment", nameOfTestDeployment,
					nameOfDeceptionPolicy, deceptionPolicy.Spec.Traps, &allFilesystemHoneytokenPaths)
			}, time.Minute, time.Second).Should(Succeed())

			updateObservedFilePaths(deceptionPolicy.Spec.Traps, &allFilesystemHoneytokenPaths)

			By("validating that the honeytoken is updated in the test pod and has the expected content")
			for _, trap := range deceptionPolicy.Spec.Traps {
				err := verifyHoneytokenAndAwaitAlertTetragon(trap, lastModificationTime,
					testNamespace, testPodName, containersPolicyShouldMatch)
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("validating that the status conditions of the DeceptionPolicy are accurate")
			Eventually(func() error {
				return verifyStatusConditions(testNamespace, testCrdName, nameOfDeceptionPolicy, true, true)
			}, time.Minute, time.Second).Should(Succeed())
		})
	})

	When("deleting the DeceptionPolicy CR", func() {
		It("should remove the honeytoken from the test pod", func() {
			By("deleting the DeceptionPolicy CR")
			cmd := exec.Command("kubectl", "delete", testCrdName, nameOfDeceptionPolicy)
			_, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the DeceptionPolicy CR is deleted")
			verifyDeceptionPolicyDeleted := func() error {
				// Get the DeceptionPolicy CR
				cmd = exec.Command("kubectl", "get", testCrdName, nameOfDeceptionPolicy, "-o", "json")
				_, err = testutils.Run(cmd)
				if err == nil { // We expect an error here, as the CR should not exist anymore
					return fmt.Errorf("DeceptionPolicy CR not deleted yet")
				}
				return nil
			}
			Eventually(verifyDeceptionPolicyDeleted, time.Minute, time.Second).Should(Succeed())

			By("validating that the test pod is running as expected")
			Expect(waitDeploymentReady(testNamespace, nameOfTestDeployment)).To(Succeed())
			Eventually(func() error {
				return verifyTestPodRunningByLabel(testNamespace, labelOfTestDeployment, &testPodName)
			}, time.Minute, time.Second).Should(Succeed())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is not present in the test pod")
			Eventually(func() error {
				return verifyAnnotationPresentInPod(testNamespace, testPodName)
			}).ShouldNot(Succeed())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is not present in the test deployment")
			Eventually(func() error {
				return verifyAnnotationPresentInDeployment(testNamespace, nameOfTestDeployment)
			}).ShouldNot(Succeed())

			By("validating that the honeytokens are removed from the test pod")
			for _, filePath := range allFilesystemHoneytokenPaths {
				Eventually(func() error {
					return verifyHoneytokenRemoved(filePath, testNamespace, testPodName, containersPolicyShouldMatch)
				}, time.Minute, time.Second).Should(Succeed())
			}
		})
	})

	When("applying a DeceptionPolicy CR with mutateExisting=false", func() {
		It("should not attempt to place any traps", func() {
			By("adding the DeceptionPolicy CR")
			cmd := exec.Command("kubectl", "apply", "-n", testNamespace,
				"-f", filepath.Join(projectDir, yamlOfFilesystokenNoMutateExisting))
			_, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is not present in the test pod")
			Eventually(func() error {
				return verifyAnnotationPresentInPod(testNamespace, testPodName)
			}).ShouldNot(Succeed())

			By("validating that the annotation " + constants.AnnotationKeyChanges + " is not present in the test deployment")
			Eventually(func() error {
				return verifyAnnotationPresentInDeployment(testNamespace, nameOfTestDeployment)
			}).ShouldNot(Succeed())

			By("validating that no honeytokens exist in the test pods")
			for _, filePath := range allFilesystemHoneytokenPaths {
				Eventually(func() error {
					return verifyHoneytokenRemoved(filePath, testNamespace, testPodName, containersPolicyShouldMatch)
				}, time.Minute, time.Second).Should(Succeed())
			}

			By("validating that the status conditions of the DeceptionPolicy show that no decoys are deployed")
			Eventually(func() error {
				return verifyStatusConditions(testNamespace, testCrdName, nameOfDeceptionPolicy, false, true)
			}, time.Minute, time.Second).Should(Succeed())
		})
	})

	When("deleting the test pods", func() {
		It("should delete the test pods", func() {
			By("deleting the test pod")
			cmd := exec.Command("kubectl", "delete", "-n", testNamespace,
				"-f", filepath.Join(projectDir, yamlOfTestDeployment))
			_, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("deleting the extra test pod")
			cmd = exec.Command("kubectl", "delete", "-n", testNamespace,
				"-f", filepath.Join(projectDir, yamlOfExtraTestPod))
			_, err = testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("validating that the test pod is deleted")
			verifyTestPodDeleted := func() error {
				// Get the name of the test pod
				podNames, err := testutils.GetPodNames(testNamespace, labelOfTestDeployment)
				if err != nil {
					return err
				}
				if len(podNames) != 0 {
					return fmt.Errorf("expect 0 test pod running, but got %d", len(podNames))
				}
				return nil
			}
			Eventually(verifyTestPodDeleted, time.Minute, time.Second).Should(Succeed())
		})
	})

	When("deleting the controller-manager", func() {
		It("should delete the controller-manager", func() {
			By("deleting the controller-manager")
			cmd := exec.Command("make", "undeploy")
			_, err := testutils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
