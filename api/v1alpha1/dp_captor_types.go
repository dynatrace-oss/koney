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

// CaptorDeployment is the entity that monitors access to the traps.
type CaptorDeployment struct {
	// Strategy is the technical method to deploy the captor.
	// Currently, only "tetragon" is supported, which is also the default.
	// It requires the Tetragon controller to be installed.
	// +kubebuilder:validation:Enum=tetragon;kive
	// +optional
	// +kubebuilder:default="tetragon"
	Strategy string `json:"strategy,omitempty" yaml:"strategy,omitempty"`
}
