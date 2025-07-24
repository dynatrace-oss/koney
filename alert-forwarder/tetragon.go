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

package main

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const (
	// TetragonTracingPoliciesGVP - group, version, plural of the Tetragon TracingPolicy CRD
	TetragonTracingPoliciesGroup   = "cilium.io"
	TetragonTracingPoliciesVersion = "v1alpha1"
	TetragonTracingPoliciesPlural  = "tracingpolicies"

	// TetragonNamespace is the namespace where Tetragon is assumed to be running
	TetragonNamespace = "kube-system"
	// TetragonPolicyPrefix is the prefix for all tracing policies created by Koney
	TetragonPolicyPrefix = "koney-tracing-policy-"
	// TetragonPodLabelSelector is the label selector to find Tetragon pods
	TetragonPodLabelSelector = "app.kubernetes.io/name=tetragon"
	// TetragonPodContainerName is the container name where Tetragon logs are written
	TetragonPodContainerName = "export-stdout"
	// TetragonDeceptionPolicyRef is the label key that references the deception policy in a tracing policy
	TetragonDeceptionPolicyRef = "koney/deception-policy"
)

var (
	// eventCache stores hashes of already processed events to prevent duplicates
	eventCache = sync.Map{}
	// timePattern matches timestamp with nanoseconds in JSON
	timePattern = regexp.MustCompile(`("time":"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})\.\d{9}(Z")`)
)

// Represents a raw Tetragon event
type TetragonEvent map[string]interface{}

// Reads Tetragon events from pod logs
func ReadTetragonEvents(kubeClient *kubernetes.Clientset, sinceSeconds int64) (map[string][]TetragonEvent, error) {
	ctx := context.Background()

	Debug("Looking for Tetragon pods with label selector: %s", TetragonPodLabelSelector)

	podList, err := kubeClient.CoreV1().Pods(TetragonNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: TetragonPodLabelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Tetragon pods: %w", err)
	}

	Debug("Found %d Tetragon pods", len(podList.Items))

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no Tetragon pods found with selector %s in namespace %s", TetragonPodLabelSelector, TetragonNamespace)
	}

	eventsPerPolicy := make(map[string][]TetragonEvent)

	for i, pod := range podList.Items {
		Debug("Processing pod %d/%d: %s (status: %s)", i+1, len(podList.Items), pod.Name, pod.Status.Phase)

		if pod.Status.Phase != v1.PodPhase(v1.PodRunning) {
			Warn("Skipping pod %s - not in running state (status: %s)", pod.Name, pod.Status.Phase)
			continue
		}

		logOptions := &v1.PodLogOptions{
			Container:    TetragonPodContainerName,
			SinceSeconds: &sinceSeconds,
		}

		Debug("Fetching logs from pod %s, container %s, since %d seconds", pod.Name, TetragonPodContainerName, sinceSeconds)

		req := kubeClient.CoreV1().Pods(TetragonNamespace).GetLogs(pod.Name, logOptions)
		stream, err := req.Stream(ctx)
		if err != nil {
			Error("Failed to get logs from pod %s: %v", pod.Name, err)
			continue
		}
		defer stream.Close()

		// Counters for debugging
		lineCount := 0
		matchCount := 0
		eventCount := 0

		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			line := scanner.Text()
			lineCount++

			if !strings.Contains(line, TetragonPolicyPrefix) {
				continue
			}
			matchCount++

			Debug("Found potential Tetragon event line (match %d): %s", matchCount, line[:min(100, len(line))])

			line = timePattern.ReplaceAllString(line, "$1$2")

			var event TetragonEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				Warn("Failed to parse JSON from line: %v", err)
				continue
			}

			policyName := extractTracingPolicyName(event)
			if policyName == nil || !strings.HasPrefix(*policyName, TetragonPolicyPrefix) {
				Debug("Skipping event - no valid policy name found")
				continue
			}

			eventHash := fmt.Sprintf("%x", md5.Sum([]byte(line)))
			if _, exists := eventCache.LoadOrStore(eventHash, true); exists {
				Debug("Skipping duplicate event (hash: %s)", eventHash[:8])
				continue
			}

			eventsPerPolicy[*policyName] = append(eventsPerPolicy[*policyName], event)
			eventCount++
			Debug("Added event %d for policy %s", eventCount, *policyName)
		}

		if err := scanner.Err(); err != nil {
			Error("Scanner error while reading Tetragon events from pod %s: %v", pod.Name, err)
		}

		Debug("Pod %s processed - Lines: %d, Matches: %d, Events: %d", pod.Name, lineCount, matchCount, eventCount)
	}

	// Log summary
	totalEvents := 0
	for policyName, events := range eventsPerPolicy {
		totalEvents += len(events)
		Debug("Policy %s has %d events", policyName, len(events))
	}

	Debug("Total events collected: %d across %d policies", totalEvents, len(eventsPerPolicy))
	return eventsPerPolicy, nil
}

// Extracts metadata from a process_kprobe event for filesystem honeytoken traps
func extractMetadataForFilesystemHoneytoken(processKprobe map[string]interface{}) map[string]interface{} {
	fileAccessFns := []string{"security_file_permission", "security_mmap_file"}

	functionName, _ := processKprobe["function_name"].(string)
	if !slices.Contains(fileAccessFns, functionName) {
		Debug("Function %s not in file access functions list", functionName)
		return nil
	}

	Debug("Extracting filesystem honeytoken metadata for function: %s", functionName)

	// Extract file path from args[0].file_arg.path
	args, ok := processKprobe["args"].([]interface{})
	if !ok || len(args) == 0 {
		Debug("No args found in process_kprobe")
		return map[string]interface{}{"file_path": nil}
	}

	arg0, ok := args[0].(map[string]interface{})
	if !ok {
		Debug("First arg is not a map")
		return map[string]interface{}{"file_path": nil}
	}

	fileArg, ok := arg0["file_arg"].(map[string]interface{})
	if !ok {
		Debug("No file_arg found in first argument")
		return map[string]interface{}{"file_path": nil}
	}

	filePath, _ := fileArg["path"].(string)
	Debug("Extracted file path: %s", filePath)
	return map[string]interface{}{"file_path": filePath}
}

// Maps a Tetragon event to a KoneyAlert
func MapTetragonEvent(kubeClient *kubernetes.Clientset, dynamicClient dynamic.Interface, event TetragonEvent) KoneyAlert {
	var deceptionPolicyName *string
	trapType := TrapTypeUnknown
	metadata := make(map[string]interface{})

	Debug("Mapping Tetragon event to KoneyAlert")

	// Attempt to resolve the DeceptionPolicy name
	if tracingPolicyName := extractTracingPolicyName(event); tracingPolicyName != nil {
		Debug("Found tracing policy name: %s", *tracingPolicyName)
		if name, err := resolveDeceptionPolicyName(dynamicClient, *tracingPolicyName); err == nil {
			deceptionPolicyName = &name
			Debug("Resolved deception policy name: %s", name)
		} else {
			Warn("Failed to resolve deception policy name: %v", err)
		}
	}

	// Infer trap type and metadata by inspecting the event
	if processKprobe, ok := event["process_kprobe"].(map[string]interface{}); ok {
		Debug("Found process_kprobe in event")
		if meta := extractMetadataForFilesystemHoneytoken(processKprobe); meta != nil {
			trapType = TrapTypeFilesystemHoneytoken
			metadata = meta
			Debug("Set trap type to filesystem_honeytoken with metadata: %v", metadata)
		}
	} else {
		Debug("No process_kprobe found in event")
	}

	pod := extractPodMetadata(event)
	process := extractProcessMetadata(event)

	timestamp, _ := event["time"].(string)

	alert := KoneyAlert{
		Timestamp:           timestamp,
		DeceptionPolicyName: deceptionPolicyName,
		TrapType:            trapType,
		Metadata:            metadata,
		Pod:                 pod,
		Process:             process,
	}

	Debug("Created KoneyAlert with trap type: %s", trapType)
	return alert
}

// Checks if an event should be filtered
func IsFilteredEvent(event KoneyAlert, fingerprintCode int) bool {
	if event.Process != nil && event.Process.Arguments != "" {
		fingerprints := []string{
			EncodeFingerprintInEcho(fingerprintCode),
			EncodeFingerprintInCat(fingerprintCode),
		}

		// If any fingerprint is present, filter this event
		for _, fp := range fingerprints {
			if strings.Contains(event.Process.Arguments, fp) {
				Debug("Event filtered due to fingerprint match: %s", fp)
				return true
			}
		}
	}

	return false
}

// Resolves the deception policy name from a tracing policy
func resolveDeceptionPolicyName(dynamicClient dynamic.Interface, tracingPolicyName string) (string, error) {
	ctx := context.Background()

	gvr := schema.GroupVersionResource{
		Group:    TetragonTracingPoliciesGroup,
		Version:  TetragonTracingPoliciesVersion,
		Resource: TetragonTracingPoliciesPlural,
	}

	Debug("Attempting to resolve deception policy for tracing policy: %s", tracingPolicyName)

	// Get the tracing policy
	tp, err := dynamicClient.Resource(gvr).Get(ctx, tracingPolicyName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get tracing policy %s: %w", tracingPolicyName, err)
	}

	labels, _, err := unstructured.NestedStringMap(tp.Object, "metadata", "labels")
	if err != nil {
		return "", fmt.Errorf("failed to extract labels from tracing policy %s: %w", tracingPolicyName, err)
	}

	deceptionPolicy, ok := labels[TetragonDeceptionPolicyRef]
	if !ok {
		return "", fmt.Errorf("deception policy reference not found in labels of tracing policy %s", tracingPolicyName)
	}

	Debug("Successfully resolved deception policy: %s", deceptionPolicy)
	return deceptionPolicy, nil
}

// Extracts the tracing policy name from an event
func extractTracingPolicyName(event TetragonEvent) *string {
	// Keys might be process_kprobe, process_uprobe, ...
	for key, value := range event {
		if m, ok := value.(map[string]interface{}); ok {
			if policyName, ok := m["policy_name"].(string); ok {
				Debug("Found policy name '%s' in event key '%s'", policyName, key)
				return &policyName
			}
		}
	}
	Debug("No policy name found in event")
	return nil
}

// Extracts pod metadata from an event
func extractPodMetadata(event TetragonEvent) *PodMetadata {
	// Keys might be process_kprobe, process_uprobe, ...
	for _, value := range event {
		if m, ok := value.(map[string]interface{}); ok {
			if process, ok := m["process"].(map[string]interface{}); ok {
				if pod, ok := process["pod"].(map[string]interface{}); ok {
					metadata := &PodMetadata{
						Name:      getStringValue(pod, "name"),
						Namespace: getStringValue(pod, "namespace"),
					}

					if container, ok := pod["container"].(map[string]interface{}); ok {
						metadata.Container = ContainerMetadata{
							ID:   getStringValue(container, "id"),
							Name: getStringValue(container, "name"),
						}
					}

					Debug("Extracted pod metadata: %s/%s", metadata.Namespace, metadata.Name)
					return metadata
				}
			}
		}
	}
	Debug("No pod metadata found in event")
	return nil
}

// Extracts process metadata from an event
func extractProcessMetadata(event TetragonEvent) *ProcessMetadata {
	// Keys might be process_kprobe, process_uprobe, ...
	for _, value := range event {
		if m, ok := value.(map[string]interface{}); ok {
			if process, ok := m["process"].(map[string]interface{}); ok {
				metadata := &ProcessMetadata{
					PID:       getIntValue(process, "pid"),
					CWD:       getStringValue(process, "cwd"),
					Binary:    getStringValue(process, "binary"),
					Arguments: getStringValue(process, "arguments"),
				}
				Debug("Extracted process metadata: %s (PID: %d)", metadata.Binary, metadata.PID)
				return metadata
			}
		}
	}
	Debug("No process metadata found in event")
	return nil
}

// Helper Functions
func getStringValue(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

func getIntValue(m map[string]interface{}, key string) int {
	if val, ok := m[key].(float64); ok {
		return int(val)
	}
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
