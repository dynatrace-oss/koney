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

package forwarder

import (
	"bufio"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	ciliumiov1alpha1 "github.com/cilium/tetragon/pkg/k8s/apis/cilium.io/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8slog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dynatrace-oss/koney/internal/controller/constants"
	"github.com/dynatrace-oss/koney/internal/controller/utils"
)

const (
	// tetragonNamespace is the namespace where Tetragon is assumed to be running.
	tetragonNamespace = "kube-system"

	// tetragonPodLabelSelector finds the Tetragon pods.
	tetragonPodLabelSelector = "app.kubernetes.io/name=tetragon"

	// tetragonPodContainerName is the container that writes the Tetragon events.
	tetragonPodContainerName = "export-stdout"

	// tetragonPolicyPrefix is the prefix of all tracing policies that Koney creates.
	tetragonPolicyPrefix = "koney-tracing-policy-"

	// minLogWindowSeconds is the age of the Tetragon log lines that are read on a pull
	// that follows shortly after the previous one.
	minLogWindowSeconds = 60

	// maxLogWindowSeconds caps how far a pull looks back after a long quiet period.
	maxLogWindowSeconds = 900

	// logWindowSlackSeconds is added to the time that passed since the previous pull.
	logWindowSlackSeconds = 15

	// fileAccessFunction and fileAccessMmapFunction are the kprobes that a filesystem honeytoken trap hooks.
	fileAccessFunction     = "security_file_permission"
	fileAccessMmapFunction = "security_mmap_file"
)

// subSecondPattern matches the sub-second part of a Tetragon event timestamp. Events are
// often duplicated because kprobes can trigger multiple times, so events that are identical
// within the same second are treated as one.
var subSecondPattern = regexp.MustCompile(`("time":"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})\.\d{9}(Z")`)

// tetragonEvent is one line of the Tetragon event log. Only the parts that Koney reads are typed.
type tetragonEvent struct {
	Time     string          `json:"time"`
	NodeName string          `json:"node_name"`
	Kprobe   *tetragonKprobe `json:"process_kprobe"`
}

type tetragonKprobe struct {
	PolicyName   string              `json:"policy_name"`
	FunctionName string              `json:"function_name"`
	Process      *tetragonProcess    `json:"process"`
	Args         []tetragonKprobeArg `json:"args"`
}

type tetragonProcess struct {
	UID       *int         `json:"uid"`
	PID       *int         `json:"pid"`
	Cwd       *string      `json:"cwd"`
	Binary    *string      `json:"binary"`
	Arguments *string      `json:"arguments"`
	Pod       *tetragonPod `json:"pod"`
}

type tetragonPod struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Container struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"container"`
}

type tetragonKprobeArg struct {
	FileArg *struct {
		Path string `json:"path"`
	} `json:"file_arg"`
}

// readTetragonEvents reads the recent Tetragon events of all Tetragon pods,
// and returns the events of Koney tracing policies, grouped by the policy name.
func (f *Forwarder) readTetragonEvents(ctx context.Context) map[string][]tetragonEvent {
	log := k8slog.FromContext(ctx)

	podList := &corev1.PodList{}
	selector, err := labelSelector(tetragonPodLabelSelector)
	if err != nil {
		log.Error(err, "Unable to build the Tetragon pod selector")
		return nil
	}

	if err := f.Client.List(ctx, podList, client.InNamespace(tetragonNamespace), selector); err != nil {
		log.Error(err, "Unable to list the Tetragon pods")
		return nil
	}

	sinceSeconds := f.logWindow()

	eventsPerPolicy := map[string][]tetragonEvent{}
	for _, pod := range podList.Items {
		logs, err := f.readPodLog(ctx, pod.Name, sinceSeconds)
		if err != nil {
			if !apierrors.IsNotFound(err) { // the pod might have been deleted in the meantime
				log.Error(err, "Unable to read the log of a Tetragon pod", "pod", pod.Name)
			}
			continue
		}

		f.collectEvents(ctx, pod.Name, logs, eventsPerPolicy)
	}

	return eventsPerPolicy
}

// logWindow returns the age of the Tetragon log lines to read, which covers the time
// that passed since the previous pull, so that a delayed pull does not miss events.
func (f *Forwarder) logWindow() int64 {
	now := time.Now()
	window := int64(minLogWindowSeconds)

	if !f.lastReadAt.IsZero() {
		elapsed := int64(now.Sub(f.lastReadAt).Seconds()) + logWindowSlackSeconds
		window = max(window, min(elapsed, maxLogWindowSeconds))
	}
	f.lastReadAt = now

	return window
}

func (f *Forwarder) collectEvents(ctx context.Context, podName, logs string, eventsPerPolicy map[string][]tetragonEvent) {
	scanner := bufio.NewScanner(strings.NewReader(logs))
	scanner.Buffer(make([]byte, 0, 64*1024), maxLogLineSize)

	for scanner.Scan() {
		line := scanner.Text()

		// quickly filter-out lines that cannot match
		if !strings.Contains(line, tetragonPolicyPrefix) {
			continue
		}

		line = subSecondPattern.ReplaceAllString(line, "$1$2")

		var event tetragonEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue // skip non-json lines in the logs
		}

		if event.Kprobe == nil || !strings.HasPrefix(event.Kprobe.PolicyName, tetragonPolicyPrefix) {
			continue
		}

		if f.events.seen(line) {
			continue
		}

		eventsPerPolicy[event.Kprobe.PolicyName] = append(eventsPerPolicy[event.Kprobe.PolicyName], event)
	}

	// a line longer than the buffer ends the scan, so the rest of that log is lost
	if err := scanner.Err(); err != nil {
		k8slog.FromContext(ctx).Error(err, "Unable to read the log of a Tetragon pod to its end", "pod", podName)
	}
}

// mapTetragonEvent maps a Tetragon event to a Koney alert. Only kprobe events are read,
// because the tracing policies that Koney creates only ever hook kprobes.
func (f *Forwarder) mapTetragonEvent(ctx context.Context, event tetragonEvent) KoneyAlert {
	alert := KoneyAlert{
		Timestamp: event.Time,
		TrapType:  TrapTypeUnknown,
		Metadata:  map[string]any{},
	}

	if event.Kprobe == nil {
		return alert
	}

	alert.DeceptionPolicyName = f.resolveDeceptionPolicyName(ctx, event.Kprobe.PolicyName)

	if filePath, ok := fileAccessPath(event.Kprobe); ok {
		alert.TrapType = TrapTypeFilesystemHoneytoken
		alert.Metadata["file_path"] = filePath
	}

	if event.NodeName != "" {
		alert.Node = &NodeMetadata{Name: event.NodeName}
	}

	if process := event.Kprobe.Process; process != nil {
		alert.Process = &ProcessMetadata{
			UID:       process.UID,
			PID:       process.PID,
			Cwd:       process.Cwd,
			Binary:    process.Binary,
			Arguments: process.Arguments,
		}

		if pod := process.Pod; pod != nil {
			alert.Pod = &PodMetadata{
				Name:      pod.Name,
				Namespace: pod.Namespace,
				Container: ContainerMetadata{
					ID:   normalizeContainerID(pod.Container.ID),
					Name: pod.Container.Name,
				},
			}
		}
	}

	return alert
}

// isOwnAlert reports whether an alert was triggered by Koney itself,
// which is the case when the process arguments carry the fingerprint of this controller.
func isOwnAlert(alert KoneyAlert) bool {
	if alert.Process == nil || alert.Process.Arguments == nil || *alert.Process.Arguments == "" {
		return false // cannot decide, assume it is not our own
	}

	fingerprint := utils.KoneyFingerprint
	arguments := *alert.Process.Arguments

	return strings.Contains(arguments, utils.EncodeFingerprintInEcho(fingerprint)) ||
		strings.Contains(arguments, utils.EncodeFingerprintInCat(fingerprint))
}

// resolveContainerSelectors reads the container selectors that a tracing policy was created for.
// They are needed when Tetragon cannot evaluate the selectors itself, and returns nil if there are none.
func (f *Forwarder) resolveContainerSelectors(ctx context.Context, tracingPolicyName string) []string {
	tracingPolicy, err := f.getTracingPolicy(ctx, tracingPolicyName)
	if err != nil {
		return nil
	}

	encoded := tracingPolicy.Annotations[constants.AnnotationKeyContainerSelectors]
	if encoded == "" {
		return nil
	}

	var selectors []string
	if err := json.Unmarshal([]byte(encoded), &selectors); err != nil {
		return nil
	}

	return selectors
}

// containerMatchesSelectors reports whether a container name matches any of the given selectors.
func containerMatchesSelectors(containerName string, selectors []string) bool {
	if containerName == "" {
		return false
	}

	for _, selector := range selectors {
		if matched, err := utils.MatchContainerName(selector, containerName); err == nil && matched {
			return true
		}
	}

	return false
}

func (f *Forwarder) resolveDeceptionPolicyName(ctx context.Context, tracingPolicyName string) *string {
	tracingPolicy, err := f.getTracingPolicy(ctx, tracingPolicyName)
	if err != nil {
		return nil
	}

	name, ok := tracingPolicy.Labels[constants.LabelKeyDeceptionPolicyRef]
	if !ok {
		return nil
	}

	return &name
}

func (f *Forwarder) getTracingPolicy(ctx context.Context, name string) (*ciliumiov1alpha1.TracingPolicy, error) {
	tracingPolicy := &ciliumiov1alpha1.TracingPolicy{}
	if err := f.Client.Get(ctx, client.ObjectKey{Name: name}, tracingPolicy); err != nil {
		if !apierrors.IsNotFound(err) { // the policy might have been deleted in the meantime
			k8slog.FromContext(ctx).Error(err, "Unable to read a Tetragon tracing policy", "policy", name)
		}
		return nil, err
	}

	return tracingPolicy, nil
}

func fileAccessPath(kprobe *tetragonKprobe) (string, bool) {
	if kprobe.FunctionName != fileAccessFunction && kprobe.FunctionName != fileAccessMmapFunction {
		return "", false
	}

	if len(kprobe.Args) == 0 || kprobe.Args[0].FileArg == nil {
		return "", true
	}

	return kprobe.Args[0].FileArg.Path, true
}
