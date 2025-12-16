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

// ChangeAnnotation stores changes made by Koney to an object.
type ChangeAnnotation struct {
	// DeceptionPolicyName is the name of the DeceptionPolicy that was applied to the object.
	DeceptionPolicyName string `json:"deceptionPolicyName"`

	// Traps is the list of traps that were added to the object.
	Traps []TrapAnnotation `json:"traps"`
}

// TrapAnnotation stores the information of a trap that was added to some object.
type TrapAnnotation struct {
	// DeploymentStrategy is the strategy to deploy the trap.
	DeploymentStrategy string `json:"deploymentStrategy"`

	// Containers is the list of containers where the trap is deployed.
	// +kubebuilder:validation:UniqueItems=true
	Containers []string `json:"containers"`

	// CreatedAt is the time in the current local time when the trap was injected in the pod.
	// +kubebuilder:validation:Format=date-time
	CreatedAt string `json:"createdAt"`

	// UpdatedAt is the time in the current local time when the trap was last updated in the pod.
	// +kubebuilder:validation:Format=date-time
	// +optional
	UpdatedAt string `json:"updatedAt"`

	// FilesystemHoneytoken is the configuration for a filesystem honeytoken trap.
	// +optional
	FilesystemHoneytoken FilesystemHoneytokenAnnotation `json:"filesystemHoneytoken"`

	// HttpEndpoint is the configuration for an HTTP endpoint trap.
	// +optional
	HttpEndpoint HttpEndpointAnnotation `json:"httpEndpoint"`

	// HttpPayload is the configuration for an HTTP payload trap.
	// +optional
	HttpPayload HttpPayloadAnnotation `json:"httpPayload"`
}

// FilesystemHoneytokenAnnotation represents a concrete deployment of a filesystem honeytoken trap.
type FilesystemHoneytokenAnnotation struct {
	// FilePath is the absolute path to the honeytoken file.
	FilePath string `json:"filePath"`

	// FileContentHash is the MD5 hash of the file content.
	FileContentHash string `json:"fileContentHash"`

	// ReadOnly is true if the file is read-only.
	ReadOnly bool `json:"readOnly"`
}

// Equals returns true if the filesystem honeytoken annotations are equal.
func (annotation *FilesystemHoneytokenAnnotation) Equals(other *FilesystemHoneytokenAnnotation) bool {
	if annotation == other {
		return true
	}
	if annotation.FilePath != other.FilePath {
		return false
	}
	if annotation.FileContentHash != other.FileContentHash {
		return false
	}
	if annotation.ReadOnly != other.ReadOnly {
		return false
	}

	return true
}

// HttpEndpointAnnotation represents a concrete deployment of an HTTP endpoint trap.
type HttpEndpointAnnotation struct {
	// TODO: Implement.
}

// Equals returns true if the HTTP endpoint annotations are equal.
func (annotation *HttpEndpointAnnotation) Equals(other *HttpEndpointAnnotation) bool {
	// TODO: Implement.
	return true
}

// AnnotationHttpEndpoint represents a concrete deployment of an HTTP payload trap.
type HttpPayloadAnnotation struct {
	// TODO: Implement.
}

// Equals returns true if the HTTP payload annotations are equal.
func (annotation *HttpPayloadAnnotation) Equals(other *HttpPayloadAnnotation) bool {
	// TODO: Implement.
	return true
}

// TrapType translates a TrapAnnotation to a TrapType.
func (trap *TrapAnnotation) TrapType() TrapType {
	switch {
	case trap.FilesystemHoneytoken != FilesystemHoneytokenAnnotation{}:
		return FilesystemHoneytokenTrap
	case trap.HttpEndpoint != HttpEndpointAnnotation{}:
		return HttpEndpointTrap
	case trap.HttpPayload != HttpPayloadAnnotation{}:
		return HttpPayloadTrap
	default:
		return UnknownTrap
	}
}

// Equals returns true if the traps annotations are equal (excluding CreatedAt and UpdatedAt).
// If ignoreContainers is true, the function also ignores the containers list.
func (annotation *TrapAnnotation) Equals(other *TrapAnnotation, ignoreContainers bool) bool {
	if annotation == other {
		return true
	}
	if annotation.DeploymentStrategy != other.DeploymentStrategy {
		return false
	}

	if !ignoreContainers {
		if len(annotation.Containers) != len(other.Containers) {
			return false
		}
		for i, container := range annotation.Containers {
			if container != other.Containers[i] {
				return false
			}
		}
	}

	switch annotation.TrapType() {
	case FilesystemHoneytokenTrap:
		if !annotation.FilesystemHoneytoken.Equals(&other.FilesystemHoneytoken) {
			return false
		}
	case HttpEndpointTrap:
		if !annotation.HttpEndpoint.Equals(&other.HttpEndpoint) {
			return false
		}
	case HttpPayloadTrap:
		if !annotation.HttpPayload.Equals(&other.HttpPayload) {
			return false
		}
	default:
		return false
	}

	return true
}
