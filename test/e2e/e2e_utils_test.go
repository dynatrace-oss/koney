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
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller"
	"github.com/dynatrace-oss/koney/internal/controller/constants"
	"github.com/dynatrace-oss/koney/internal/controller/utils"
	testutils "github.com/dynatrace-oss/koney/test/utils"
)

// KoneyAlert represents the structure of a Koney alert that we expect to see in the logs
type KoneyAlert struct {
	Timestamp           string            `json:"timestamp"`
	DeceptionPolicyName string            `json:"deception_policy_name"`
	TrapType            string            `json:"trap_type"`
	Metadata            map[string]string `json:"metadata"`
	Pod                 struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Container struct {
			Id   string `json:"id"`
			Name string `json:"name"`
		} `json:"container"`
	} `json:"pod"`
	Node struct {
		Name string `json:"name"`
	}
	Process struct {
		Uid       int    `json:"uid"`
		Pid       int    `json:"pid"`
		Cwd       string `json:"cwd"`
		Binary    string `json:"binary"`
		Arguments string `json:"arguments"`
	} `json:"process"`
}

// updateObservedFilePaths updates the list of file paths observed during the tests
func updateObservedFilePaths(traps []v1alpha1.Trap, filePaths *[]string) {
	// Add the file path to the list of file paths if not already present
	for _, trap := range traps {
		if trap.TrapType() == v1alpha1.FilesystemHoneytokenTrap {
			if !utils.Contains(*filePaths, trap.FilesystemHoneytoken.FilePath) {
				*filePaths = append(*filePaths, trap.FilesystemHoneytoken.FilePath)
			}
		}
	}
}

// verifyTestPodRunningByLabel checks if the test pod is running
//
//nolint:unparam
func verifyTestPodRunningByLabel(namespace, label string, podName *string) error {
	// Get the name of the test pod
	podNames, err := testutils.GetPodNames(namespace, label)
	if err != nil {
		return err
	}
	if len(podNames) != 1 {
		return fmt.Errorf("expect 1 test pod running, but got %d", len(podNames))
	}
	*podName = podNames[0]

	return verifyTestPodRunningByName(namespace, *podName)
}

// verifyTestPodRunningByName checks if the test pod is running
func verifyTestPodRunningByName(namespace, name string) error {
	cmd := exec.Command("kubectl", "get", "-n", namespace, "pods", name,
		"-o", "jsonpath={.status.phase}")
	status, err := testutils.Run(cmd)
	if err != nil {
		return err
	}

	if string(status) != "Running" {
		return fmt.Errorf("test pod %s in %s status", name, status)
	}

	return nil
}

// waitDeploymentReady waits for the deployment to be ready
//
//nolint:unparam
func waitDeploymentReady(namespace, name string) error {
	cmd := exec.Command("kubectl", "wait", "-n", namespace, "deployment", name,
		"--for=condition=Available", "--timeout=1m")
	_, err := testutils.Run(cmd)
	return err
}

// verifyAnnotationPresentInPod checks if the changes annotation is present in the test pod
//
//nolint:unparam
func verifyAnnotationPresentInPod(namespace, name string) error {
	// Get the annotations of the test pod
	cmd := exec.Command("kubectl", "get", "-n", namespace, "pod", name,
		"-o", "jsonpath={.metadata.annotations}")
	annotation, err := testutils.Run(cmd)
	if err != nil {
		return err
	}

	// Check if the annotation changes annotation is present
	if !strings.Contains(string(annotation), constants.AnnotationKeyChanges) {
		return fmt.Errorf("annotation not present yet")
	}

	return nil
}

// verifyAnnotationPresentInDeployment checks if the changes annotation is present in the test deployment
func verifyAnnotationPresentInDeployment(namespace, name string) error {
	// Get the annotations of the test deployment
	cmd := exec.Command("kubectl", "get", "-n", namespace, "deployment", name,
		"-o", "jsonpath={.metadata.annotations}")
	annotation, err := testutils.Run(cmd)
	if err != nil {
		return err
	}

	// Check if the changes annotation is present
	if !strings.Contains(string(annotation), constants.AnnotationKeyChanges) {
		return fmt.Errorf("annotation not present yet")
	}

	return nil
}

// verifyAnnotationIsAccurate checks if the changes annotation is present and accurate in the test pod
//
//nolint:unparam
func verifyAnnotationIsAccurate(
	namespace, resourceKind, resourceName, deceptionPolicyName string,
	traps []v1alpha1.Trap, allPaths *[]string,
) error {
	cmd := exec.Command("kubectl", "get", "-n", namespace, resourceKind, resourceName,
		"-o", "jsonpath={.metadata.annotations."+constants.AnnotationKeyChanges+"}")
	annotation, err := testutils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	var existingAnnotation []v1alpha1.ChangeAnnotation
	if err := json.Unmarshal(annotation, &existingAnnotation); err != nil {
		return err
	}

	if len(existingAnnotation) != 1 {
		return fmt.Errorf("expected 1 annotation change, but got %d",
			len(existingAnnotation))
	}
	if existingAnnotation[0].DeceptionPolicyName != deceptionPolicyName {
		return fmt.Errorf("expected DeceptionPolicyName to be %s, but got %s",
			deceptionPolicyName, existingAnnotation[0].DeceptionPolicyName)
	}
	if len(existingAnnotation[0].Traps) != len(traps) {
		return fmt.Errorf("expected %d trap(s), but got %d",
			len(traps), len(existingAnnotation[0].Traps))
	}
	for index, trap := range traps {
		switch trap.TrapType() {
		case v1alpha1.FilesystemHoneytokenTrap:
			if existingAnnotation[0].Traps[index].FilesystemHoneytoken.FilePath != trap.FilesystemHoneytoken.FilePath {
				return fmt.Errorf("expected FilePath to be %s, but got %s",
					trap.FilesystemHoneytoken.FilePath,
					existingAnnotation[0].Traps[index].FilesystemHoneytoken.FilePath)
			}
			if existingAnnotation[0].Traps[index].DeploymentStrategy != trap.DecoyDeployment.Strategy {
				return fmt.Errorf("expected DeploymentStrategy to be %s, but got %s",
					trap.DecoyDeployment.Strategy,
					existingAnnotation[0].Traps[index].DeploymentStrategy)
			}
		default:
			return fmt.Errorf("trap type %s not supported", trap.TrapType())
		}
	}

	// Add the file path to the list of file paths if not already present
	for _, trap := range traps {
		if trap.TrapType() == v1alpha1.FilesystemHoneytokenTrap {
			if !utils.Contains(*allPaths, trap.FilesystemHoneytoken.FilePath) {
				*allPaths = append(*allPaths, trap.FilesystemHoneytoken.FilePath)
			}
		}
	}

	return nil
}

// verifyStatusConditions checks if the status conditions of the DeceptionPolicy are as expected
//
//nolint:unparam
func verifyStatusConditions(namespace, crdName, deceptionPolicyName string, expectDecoys, expectCaptors bool) error {
	// Get the DeceptionPolicy CR to get the expected value of the annotation
	var deceptionPolicy v1alpha1.DeceptionPolicy
	cmd := exec.Command("kubectl", "get", crdName, deceptionPolicyName, "-o", "json", "-n", namespace)
	deceptionPolicyJSON, err := testutils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())
	err = json.Unmarshal(deceptionPolicyJSON, &deceptionPolicy)
	Expect(err).NotTo(HaveOccurred())

	numberOfTraps := len(deceptionPolicy.Spec.Traps)

	resourceFound := deceptionPolicy.Status.GetCondition(controller.ResourceFoundType)
	Expect(resourceFound).NotTo(BeNil())
	Expect(resourceFound.Status).To(Equal(metav1.ConditionTrue))
	Expect(resourceFound.Reason).To(Equal(controller.ResourceFoundReason_Found))
	Expect(resourceFound.Message).To(Equal(controller.ResourceFoundMessage_Found))

	policyValid := deceptionPolicy.Status.GetCondition(controller.PolicyValidType)
	Expect(policyValid).NotTo(BeNil())
	Expect(policyValid.Status).To(Equal(metav1.ConditionTrue))
	Expect(policyValid.Reason).To(Equal(controller.PolicyValidReason_Valid))
	Expect(policyValid.Message).To(Equal(fmt.Sprintf("%d/%d traps are valid", numberOfTraps, numberOfTraps)))

	decoysDeployed := deceptionPolicy.Status.GetCondition(controller.DecoysDeployedType)
	Expect(decoysDeployed).NotTo(BeNil())
	if expectDecoys {
		Expect(decoysDeployed.Status).To(Equal(metav1.ConditionTrue))
		Expect(decoysDeployed.Reason).To(Equal(controller.DecoysDeployedReason_Success))
		expectedMessage := fmt.Sprintf("%d/%d decoys deployed (0 skipped)", numberOfTraps, numberOfTraps)
		Expect(decoysDeployed.Message).To(Equal(expectedMessage))
	} else {
		Expect(decoysDeployed.Status).To(Equal(metav1.ConditionFalse))
		Expect(decoysDeployed.Reason).To(Equal(controller.DecoysDeployedReason_NoObjects))
		Expect(decoysDeployed.Message).To(Equal(controller.TrapDeployedMessage_NoObjects))

	}

	captorsDeployed := deceptionPolicy.Status.GetCondition(controller.CaptorsDeployedType)
	Expect(captorsDeployed).NotTo(BeNil())
	if expectCaptors {
		Expect(captorsDeployed.Status).To(Equal(metav1.ConditionTrue))
		Expect(captorsDeployed.Reason).To(Equal(controller.CaptorsDeployedReason_Success))
		expectedMessage := fmt.Sprintf("%d/%d captors deployed (0 skipped)", numberOfTraps, numberOfTraps)
		Expect(captorsDeployed.Message).To(Equal(expectedMessage))
	} else {
		Expect(captorsDeployed.Status).To(Equal(metav1.ConditionFalse))
		Expect(captorsDeployed.Reason).To(Equal(controller.CaptorsDeployedReason_NoObjects))
		Expect(captorsDeployed.Message).To(Equal(controller.TrapDeployedMessage_NoObjects))
	}

	// check presence of unknown conditions
	for _, condition := range deceptionPolicy.Status.Conditions {
		if condition.Type != controller.ResourceFoundType &&
			condition.Type != controller.PolicyValidType &&
			condition.Type != controller.DecoysDeployedType &&
			condition.Type != controller.CaptorsDeployedType {
			return fmt.Errorf("found unknown condition type %s", condition.Type)
		}
	}

	return nil
}

// verifyHoneytokenRemovedAndAwaitAlert accesses the honeytoken file in the test pod
// and waits for the alert to be triggered. Also, we wait for Tetragon to be ready with
// setting up probes, and give the alert forwarder some time to process the alert.
//
//nolint:unparam
func verifyHoneytokenAndAwaitAlert(
	trap v1alpha1.Trap, lastModified time.Time,
	podNamespace, podName string, containers []string,
) error {
	// Wait for Tetragon to setup probes
	pattern := "Loaded BPF maps and events for sensor successfully"
	Eventually(func() error {
		return expectLogLine(pattern, "kube-system", "app.kubernetes.io/name=tetragon", "tetragon", &lastModified)
	}, time.Minute, time.Second).Should(Succeed())

	// eBPF probes tend to need some extra time before being ready
	time.Sleep(3 * time.Second)

	accessAttempts := 0
	firstAccessTime := time.Now()

	// Try to access the honeytoken file and watch for alerts many times,
	// because eBPF events might be delayed or even dropped under kernel load
	Eventually(func() error {

		// Verify the honeytoken content (this should trigger an alert)
		err := verifyHoneytokenContent(trap, podNamespace, podName, containers)
		if err != nil {
			return err
		}

		accessAttempts += len(containers)

		// Try finding the log entry many times because the processing takes some time
		const maxAttempts = 10
		var alerts []KoneyAlert
		var attempt int

		for attempt < maxAttempts {
			alerts, err = findKoneyAlerts(trap.FilesystemHoneytoken.FilePath, managerNamespace, &firstAccessTime)
			if err != nil {
				return err
			}

			// Remove alerts that happened before the first access time
			// (we don't want delayed alerts from previous tests)
			filteredAlerts := []KoneyAlert{}
			for i := 0; i < len(alerts); i++ {
				timestamp, err := time.Parse(time.RFC3339, alerts[i].Timestamp)
				if err != nil {
					return fmt.Errorf("failed to parse alert timestamp: %v", err)
				}
				if timestamp.After(firstAccessTime.Truncate(time.Second)) {
					filteredAlerts = append(filteredAlerts, alerts[i])
				}
			}
			alerts = filteredAlerts

			// Wait 1 second and try again ...
			if len(alerts) == 0 {
				time.Sleep(time.Second)
				attempt++
				continue
			}

			// Check if the number of alerts is in range: at least as many alerts as containers,
			// but not more than the total number of access attempts that we made
			if len(alerts) < len(containers) || len(alerts) > accessAttempts {
				return fmt.Errorf("expected %d alerts, but got %d alerts", len(containers), len(alerts))
			}

			// Alerts found
			break
		}

		if len(alerts) == 0 {
			return fmt.Errorf("expected alerts not found in logs after %d attempts", maxAttempts)
		}

		for _, alert := range alerts {
			fmt.Fprintf(ginkgo.GinkgoWriter, "found alert: %+v\n", alert) //nolint:errcheck

			timestamp, err := time.Parse(time.RFC3339, alert.Timestamp)
			Expect(err).NotTo(HaveOccurred())
			Expect(func() error {
				// the first access time has millisecond precision, but Koney alerts have second precision,
				// so we need to truncate the first access time to seconds for a valid comparison
				if timestamp.Before(firstAccessTime.Truncate(time.Second)) {
					return fmt.Errorf("expected alert timestamp at %s to happen after first access at %s", timestamp, firstAccessTime)
				}
				return nil
			}()).To(Succeed())

			Expect(alert.DeceptionPolicyName).NotTo(BeEmpty())
			Expect(alert.TrapType).To(Equal("filesystem_honeytoken"))

			Expect(alert.Metadata).NotTo(BeNil())
			Expect(alert.Metadata["file_path"]).To(Equal(trap.FilesystemHoneytoken.FilePath))

			Expect(alert.Pod).NotTo(BeNil())
			Expect(alert.Pod.Name).To(Equal(podName))
			Expect(alert.Pod.Namespace).To(Equal(podNamespace))
			Expect(alert.Pod.Container.Id).NotTo(BeEmpty())
			Expect(alert.Pod.Container.Name).To(BeElementOf(containers))

			Expect(alert.Node).NotTo(BeNil())
			Expect(alert.Node.Name).NotTo(BeNil())

			Expect(alert.Process).NotTo(BeNil())
			Expect(alert.Process.Uid).To(BeZero())
			Expect(alert.Process.Pid).NotTo(BeZero())
			Expect(alert.Process.Cwd).To(Equal("/"))
			Expect(alert.Process.Binary).To(Equal("/usr/bin/cat"))
			Expect(alert.Process.Arguments).To(Equal(trap.FilesystemHoneytoken.FilePath))
		}

		return nil

	}, time.Minute, time.Second).Should(Succeed())

	return nil
}

// expectLogLine checks if the log line is present in the logs of the pod (1000 lines max)
func expectLogLine(pattern, namespace, selector, container string, sinceTime *time.Time) error {
	args := []string{"logs", "-n", namespace, "-l", selector, "-c", container, "--tail", "1000"}
	if sinceTime != nil {
		args = append(args, "--since-time", sinceTime.Format(time.RFC3339))
	}
	cmd := exec.Command("kubectl", args...)
	output, err := testutils.Run(cmd)
	if err != nil {
		return err
	}

	if _, err := regexp.MatchString(pattern, string(output)); err != nil {
		return fmt.Errorf("expected pattern '%s' not found in logs - increase tail limit?", pattern)
	}

	return nil
}

// verifyHoneytokenContent checks if the honeytoken is created in the test pod and has the expected content
func verifyHoneytokenContent(trap v1alpha1.Trap, podNamespace, podName string, containers []string) error {
	// The policy creates a file in the containers in the matchedContainers list
	for _, container := range containers {
		cmd := exec.Command("kubectl", "exec", "-n", podNamespace, podName,
			"-c", container, "--", "/usr/bin/cat", trap.FilesystemHoneytoken.FilePath)
		output, err := testutils.Run(cmd)
		if err != nil {
			return err
		}

		// Remove the newline character from the output
		stringOutput := strings.TrimSuffix(string(output), "\n")
		if stringOutput != trap.FilesystemHoneytoken.FileContent {
			return fmt.Errorf("expected %s, but got %s", trap.FilesystemHoneytoken.FileContent, stringOutput)
		}
	}

	return nil
}

// findKoneyAlerts returns log entries parsed as Koney alerts, if they contain the specified string
func findKoneyAlerts(needle, managerNamespace string, sinceTime *time.Time) ([]KoneyAlert, error) {
	args := []string{"logs", "-n", managerNamespace, "--tail", "1000",
		"-l", "control-plane=controller-manager", "-c", "alerts"}
	if sinceTime != nil {
		args = append(args, "--since-time", sinceTime.Format(time.RFC3339))
	}
	cmd := exec.Command("kubectl", args...)
	output, err := testutils.Run(cmd)
	if err != nil {
		return nil, err
	}

	alerts := []KoneyAlert{}
	needleBytes := []byte(needle)
	lines := bytes.Split(output, []byte("\n"))

	for _, line := range lines {
		if bytes.Contains(line, needleBytes) {
			var alert KoneyAlert
			if err := json.Unmarshal(line, &alert); err != nil {
				return nil, err
			}
			alerts = append(alerts, alert)
		}
	}

	return alerts, nil
}

// verifyHoneytokenRemoved checks if the honeytoken is removed from the test pod
func verifyHoneytokenRemoved(filePath string, podNamespace, podName string, containers []string) error {
	for _, container := range containers {
		cmd := exec.Command("kubectl", "exec", "-n", podNamespace, "-c", container, podName, "--", "cat", filePath)
		_, err := testutils.Run(cmd)
		if err == nil { // We expect an error here, as the file should not exist anymore
			return fmt.Errorf("honeytoken not removed yet")
		}
	}
	return nil
}
