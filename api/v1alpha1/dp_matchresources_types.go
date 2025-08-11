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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// MatchResources is used to specify resource matching criteria for a trap.
type MatchResources struct {
	// Any is a list of resource filters.
	Any []ResourceFilter `json:"any,omitempty" yaml:"any,omitempty"`
}

// ResourceFilter allow users to "AND" or "OR" between resources
type ResourceFilter struct {
	// ResourceDescription contains information about the resource being created or modified.
	ResourceDescription `json:"resources,omitempty" yaml:"resources,omitempty"`
}

type ResourceDescription struct {
	// Namespaces is a list of namespaces names.
	// It does not support wildcards.
	// +optional
	Namespaces []string `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`

	// Selector is a label selector.
	// It does not support wildcards.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty" yaml:"selector,omitempty"`

	// ContainerSelector is a selector to filter the containers to inject the trap into.
	// +optional
	// +kubebuilder:default=""
	ContainerSelector string `json:"containerSelector,omitempty" yaml:"containerSelector,omitempty"`
}
