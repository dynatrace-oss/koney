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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dynatrace-oss/koney/internal/controller/constants"
)

// kiveAlert is the alert that Kive posts to the Koney webhook.
type kiveAlert struct {
	Timestamp      string            `json:"timestamp"`
	CustomMetadata map[string]string `json:"custom-metadata"`
	Metadata       struct {
		Path string `json:"path"`
	} `json:"metadata"`
	Pod struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Container struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"container"`
	} `json:"pod"`
	Node struct {
		Name string `json:"name"`
	} `json:"node"`
	Process struct {
		UID       int    `json:"uid"`
		PID       int    `json:"pid"`
		Cwd       string `json:"cwd"`
		Binary    string `json:"binary"`
		Arguments string `json:"arguments"`
	} `json:"process"`
}

// kiveRequiredFields are the fields that every alert of Kive must carry.
var kiveRequiredFields = [][]string{
	{"timestamp"},
	{"custom-metadata", constants.MetadataKeyDeceptionPolicyName},
	{"metadata", "path"},
	{"pod", "name"}, {"pod", "namespace"}, {"pod", "container", "id"}, {"pod", "container", "name"},
	{"node", "name"},
	{"process", "uid"}, {"process", "pid"}, {"process", "cwd"}, {"process", "binary"}, {"process", "arguments"},
}

// mapKiveAlert maps an alert of Kive to a Koney alert.
func mapKiveAlert(body []byte) (KoneyAlert, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return KoneyAlert{}, fmt.Errorf("the alert is not valid json: %w", err)
	}
	if missing := missingFields(raw, kiveRequiredFields); len(missing) > 0 {
		return KoneyAlert{}, fmt.Errorf("the alert misses %s", strings.Join(missing, ", "))
	}

	var alert kiveAlert
	if err := json.Unmarshal(body, &alert); err != nil {
		return KoneyAlert{}, fmt.Errorf("the alert has unexpected types: %w", err)
	}

	policyName := alert.CustomMetadata[constants.MetadataKeyDeceptionPolicyName]

	return KoneyAlert{
		Timestamp:           alert.Timestamp,
		DeceptionPolicyName: &policyName,
		TrapType:            TrapTypeFilesystemHoneytoken,
		Metadata:            map[string]any{"file_path": alert.Metadata.Path},
		Pod: &PodMetadata{
			Name:      alert.Pod.Name,
			Namespace: alert.Pod.Namespace,
			Container: ContainerMetadata{
				ID:   normalizeContainerID(alert.Pod.Container.ID),
				Name: alert.Pod.Container.Name,
			},
		},
		Node: &NodeMetadata{Name: alert.Node.Name},
		Process: &ProcessMetadata{
			UID:       &alert.Process.UID,
			PID:       &alert.Process.PID,
			Cwd:       &alert.Process.Cwd,
			Binary:    &alert.Process.Binary,
			Arguments: &alert.Process.Arguments,
		},
	}, nil
}

func missingFields(raw map[string]any, fields [][]string) []string {
	var missing []string
	for _, path := range fields {
		var node any = raw
		for _, key := range path {
			object, ok := node.(map[string]any)
			if !ok {
				node = nil
				break
			}
			node = object[key]
		}
		if node == nil {
			missing = append(missing, strings.Join(path, "."))
		}
	}

	return missing
}
