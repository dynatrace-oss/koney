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
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

// TetragonEvent represents a raw Tetragon event
type TetragonEvent map[string]interface{}

// Reads Tetragon events from pod logs
func ReadTetragonEvents(kubeClient *kubernetes.Clientset, sinceSeconds int64) (map[string][]TetragonEvent, error) {
	ctx := context.Background()

	// List Tetragon pods
	podList, err := kubeClient.CoreV1().Pods(TetragonNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: TetragonPodLabelSelector,
	})
	if err != nil {
		return nil, err
	}

	// Debug log
	fmt.Printf("Found %d Tetragon pods", len(podList.Items))

	eventsPerPolicy := make(map[string][]TetragonEvent)

	for _, pod := range podList.Items {
		fmt.Printf("Reading logs from Tetragon pod: %s", pod.Name)

		// Get pod logs
		logOptions := &v1.PodLogOptions{
			Container:    TetragonPodContainerName,
			SinceSeconds: &sinceSeconds,
		}

		req := kubeClient.CoreV1().Pods(TetragonNamespace).GetLogs(pod.Name, logOptions)
		stream, err := req.Stream(ctx)
		if err != nil {
			fmt.Printf("Error getting logs for pod %s: %v", pod.Name, err)
			continue
		}
		defer stream.Close()

		// Counters for debugging
		lineCount := 0
		matchCount := 0

		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			line := scanner.Text()
			lineCount++

			// Filter-out lines that cannot match
			if !strings.Contains(line, TetragonPolicyPrefix) {
				continue
			}
			matchCount++

			// Milliseconds removal from timestamp for de-duplication
			line = timePattern.ReplaceAllString(line, "$1$2")

			// Parse JSON
			var event TetragonEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				fmt.Printf("Error parsing JSON: %v", err)
				continue
			}

			// Extract policy name
			policyName := extractTracingPolicyName(event)
			if policyName == nil || !strings.HasPrefix(*policyName, TetragonPolicyPrefix) {
				continue
			}

			// Avoid duplicates using hash
			eventHash := fmt.Sprintf("%x", md5.Sum([]byte(line)))
			if _, exists := eventCache.LoadOrStore(eventHash, true); exists {
				continue
			}

			eventsPerPolicy[*policyName] = append(eventsPerPolicy[*policyName], event)
		}

		if err := scanner.Err(); err != nil {
			fmt.Printf("Error reading logs for pod %s: %v", pod.Name, err)
		}

		fmt.Printf("Processed %d lines, found %d matching policy prefix from pod %s", lineCount, matchCount, pod.Name)
	}

	// Log summary
	totalEvents := 0
	for policy, events := range eventsPerPolicy {
		totalEvents += len(events)
		fmt.Printf("Policy %s has %d events", policy, len(events))
	}
	fmt.Printf("Total events collected: %d across %d policies", totalEvents, len(eventsPerPolicy))

	return eventsPerPolicy, nil
}

// Maps a Tetragon event to a KoneyAlert
func MapTetragonEvent(kubeClient *kubernetes.Clientset, event TetragonEvent) KoneyAlert {
	var deceptionPolicyName *string
	trapType := TrapTypeUnknown
	metadata := make(map[string]interface{})

	// Attempt to resolve the DeceptionPolicy name
	if tracingPolicyName := extractTracingPolicyName(event); tracingPolicyName != nil {
		if name, err := resolveDeceptionPolicyName(kubeClient, *tracingPolicyName); err == nil {
			deceptionPolicyName = &name
		}
	}

	// Infer trap type and metadata by inspecting the event
	if processKprobe, ok := event["process_kprobe"].(map[string]interface{}); ok {
		functionName, _ := processKprobe["function_name"].(string)
		fileAccessFns := []string{"security_file_permission", "security_mmap_file"}

		for _, fn := range fileAccessFns {
			if functionName == fn {
				trapType = TrapTypeFilesystemHoneytoken
				if args, ok := processKprobe["args"].([]interface{}); ok && len(args) > 0 {
					if arg0, ok := args[0].(map[string]interface{}); ok {
						if fileArg, ok := arg0["file_arg"].(map[string]interface{}); ok {
							if path, ok := fileArg["path"].(string); ok {
								metadata["file_path"] = path
							}
						}
					}
				}
				break
			}
		}
	}

	pod := extractPodMetadata(event)
	process := extractProcessMetadata(event)

	timestamp, _ := event["time"].(string)
	if timestamp == "" {
		timestamp = time.Now().Format(time.RFC3339)
	}

	return KoneyAlert{
		Timestamp:           timestamp,
		DeceptionPolicyName: deceptionPolicyName,
		TrapType:            trapType,
		Metadata:            metadata,
		Pod:                 pod,
		Process:             process,
	}
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
				return true
			}
		}
	}

	return false
}

// Resolves the deception policy name from a tracing policy
func resolveDeceptionPolicyName(kubeClient *kubernetes.Clientset, tracingPolicyName string) (string, error) {
	ctx := context.Background()

	// Create a dynamic client for the custom resource
	config, err := rest.InClusterConfig()
	if err != nil {
		return "", err
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return "", err
	}

	gvr := schema.GroupVersionResource{
		Group:    TetragonTracingPoliciesGroup,
		Version:  TetragonTracingPoliciesVersion,
		Resource: TetragonTracingPoliciesPlural,
	}

	// Get the tracing policy
	tp, err := dynamicClient.Resource(gvr).Get(ctx, tracingPolicyName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	// Extract labels
	labels, _, err := unstructured.NestedStringMap(tp.Object, "metadata", "labels")
	if err != nil {
		return "", err
	}

	deceptionPolicy, ok := labels[TetragonDeceptionPolicyRef]
	if !ok {
		return "", fmt.Errorf("deception policy reference not found")
	}

	return deceptionPolicy, nil
}

// Extracts the tracing policy name from an event
func extractTracingPolicyName(event TetragonEvent) *string {
	// Keys might be process_kprobe, process_uprobe, ...
	for _, value := range event {
		if m, ok := value.(map[string]interface{}); ok {
			if policyName, ok := m["policy_name"].(string); ok {
				return &policyName
			}
		}
	}
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

					return metadata
				}
			}
		}
	}
	return nil
}

// Extracts process metadata from an event
func extractProcessMetadata(event TetragonEvent) *ProcessMetadata {
	// Keys might be process_kprobe, process_uprobe, ...
	for _, value := range event {
		if m, ok := value.(map[string]interface{}); ok {
			if process, ok := m["process"].(map[string]interface{}); ok {
				return &ProcessMetadata{
					PID:       getIntValue(process, "pid"),
					CWD:       getStringValue(process, "cwd"),
					Binary:    getStringValue(process, "binary"),
					Arguments: getStringValue(process, "arguments"),
				}
			}
		}
	}
	return nil
}

// Helper functions
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
