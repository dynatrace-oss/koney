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
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Trap types that a KoneyAlert can report.
const (
	TrapTypeUnknown              = "unknown"
	TrapTypeFilesystemHoneytoken = "filesystem_honeytoken"
)

// ContainerMetadata describes the container where an alert was raised.
type ContainerMetadata struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PodMetadata describes the pod where an alert was raised.
type PodMetadata struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Container ContainerMetadata `json:"container"`
}

// NodeMetadata describes the node where an alert was raised.
type NodeMetadata struct {
	Name string `json:"name"`
}

// ProcessMetadata describes the process that triggered an alert.
type ProcessMetadata struct {
	UID       *int    `json:"uid"`
	PID       *int    `json:"pid"`
	Cwd       *string `json:"cwd"`
	Binary    *string `json:"binary"`
	Arguments *string `json:"arguments"`
}

// KoneyAlert is the alert that Koney writes to its log and sends to the alert sinks.
type KoneyAlert struct {
	Timestamp           string           `json:"timestamp"`
	DeceptionPolicyName *string          `json:"deception_policy_name"`
	TrapType            string           `json:"trap_type"`
	Metadata            map[string]any   `json:"metadata"`
	Pod                 *PodMetadata     `json:"pod"`
	Node                *NodeMetadata    `json:"node"`
	Process             *ProcessMetadata `json:"process"`
}

// severityRiskScores maps the severity of an alert sink to the risk score that Dynatrace expects.
var severityRiskScores = map[string]float64{
	"LOW":      3.9,
	"MEDIUM":   6.9,
	"HIGH":     8.9,
	"CRITICAL": 10.0,
}

// AlertID returns an identifier that is stable for identical alerts.
func (alert KoneyAlert) AlertID() string {
	encoded, err := json.Marshal(alert)
	if err != nil {
		encoded = []byte(fmt.Sprintf("%v", alert))
	}

	sum := md5.Sum(encoded) //nolint:gosec // the hash identifies an alert, it protects nothing
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// Description returns a human-readable description of the alert.
func (alert KoneyAlert) Description() string {
	if alert.TrapType != TrapTypeFilesystemHoneytoken {
		return "Koney alert triggered"
	}

	filePath := "?"
	if path, ok := alert.Metadata["file_path"].(string); ok && path != "" {
		filePath = path
	}

	namespacedPodName := "?"
	if alert.Pod != nil && alert.Pod.Namespace != "" && alert.Pod.Name != "" {
		namespacedPodName = alert.Pod.Namespace + "/" + alert.Pod.Name
	}

	return fmt.Sprintf("Access to honeytoken (%s) in pod (%s) detected", filePath, namespacedPodName)
}

// MapToDynatraceEvent maps an alert to the security event that the Dynatrace ingest API expects.
func MapToDynatraceEvent(alert KoneyAlert, severity string, clusterUID string) map[string]any {
	alertID := alert.AlertID()
	description := alert.Description()

	var pod PodMetadata
	if alert.Pod != nil {
		pod = *alert.Pod
	}
	var node NodeMetadata
	if alert.Node != nil {
		node = *alert.Node
	}
	var process ProcessMetadata
	if alert.Process != nil {
		process = *alert.Process
	}

	var binaryName, binaryPath any // null when the binary is unknown
	if process.Binary != nil && *process.Binary != "" {
		binaryName, binaryPath = filepath.Base(*process.Binary), filepath.Dir(*process.Binary)
	}

	var clusterUIDValue any // null when the uid could not be resolved
	if clusterUID != "" {
		clusterUIDValue = clusterUID
	}

	return map[string]any{
		"timestamp": alert.Timestamp,

		"koney.deception_policy_name": valueOrNil(alert.DeceptionPolicyName),
		"koney.trap_type":             alert.TrapType,
		"koney.metadata.file_path":    alert.Metadata["file_path"],

		"event.kind":        "SECURITY_EVENT",
		"event.type":        "DETECTION_FINDING",
		"event.name":        "Detection finding event",
		"event.provider":    "Koney",
		"event.version":     "2025-08-12",
		"event.id":          alertID,
		"event.description": description,

		"finding.type":         "KONEY_ALERT",
		"finding.id":           alertID,
		"finding.title":        description,
		"finding.description":  description,
		"finding.time.created": alert.Timestamp,
		"finding.severity":     severity,

		"dt.security.risk.level": severity,
		"dt.security.risk.score": severityRiskScores[strings.ToUpper(severity)],

		"product.name":   "Koney",
		"product.vendor": "Dynatrace Research",

		"k8s.cluster.uid":    clusterUIDValue,
		"k8s.namespace.name": pod.Namespace,
		"k8s.node.name":      node.Name,
		"k8s.pod.name":       pod.Name,
		"k8s.container.name": pod.Container.Name,
		"k8s.container.id":   pod.Container.ID,

		"process.executable.name":      binaryName,
		"process.executable.path":      binaryPath,
		"process.executable.arguments": valueOrNil(process.Arguments),
		"process.pid":                  valueOrNil(process.PID),
		"process.uid":                  valueOrNil(process.UID),
		"process.cwd":                  valueOrNil(process.Cwd),

		"object.type": "KUBERNETES_CONTAINER",
		"object.id":   pod.Container.ID,
	}
}

func valueOrNil[T any](value *T) any {
	if value == nil {
		return nil
	}

	return *value
}
