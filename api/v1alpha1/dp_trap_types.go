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

import (
	"errors"
	"fmt"
	"regexp"
)

// TrapType is a string representation of a trap type and can be used like an enum.
type TrapType string

const (
	// UnknownTrap is the default trap type.
	UnknownTrap TrapType = "Unknown"

	// FilesystemHoneytokenTrap is a filesystem honeytoken trap.
	FilesystemHoneytokenTrap TrapType = "FilesystemHoneytoken"

	// HttpEndpointTrap is an HTTP endpoint trap.
	HttpEndpointTrap TrapType = "HttpEndpoint"

	// HttpPayloadTrap is an HTTP payload trap.
	HttpPayloadTrap TrapType = "HttpPayload"
)

// Trap describes a cyber deception technique, also simply known as a trap.
type Trap struct {
	// FilesystemHoneytoken is the configuration for a filesystem honeytoken trap.
	// +optional
	FilesystemHoneytoken FilesystemHoneytoken `json:"filesystemHoneytoken,omitempty" yaml:"spec,omitempty"`

	// HttpEndpoint is the configuration for an HTTP endpoint trap.
	// +optional
	HttpEndpoint HttpEndpoint `json:"httpEndpoint,omitempty" yaml:"httpEndpoint,omitempty"`

	// HttpPayload is the configuration for an HTTP payload trap.
	// +optional
	HttpPayload HttpPayload `json:"httpPayload,omitempty" yaml:"httpPayload,omitempty"`

	// DecoyDeployment configures how traps (the entities that are attacked) are going to be deployed.
	// +optional
	DecoyDeployment DecoyDeployment `json:"decoyDeployment,omitempty" yaml:"decoyDeployment,omitempty"`

	// CaptorDeployment configures how captors (the entities that monitor access to the traps) are going to be deployed.
	// +optional
	CaptorDeployment CaptorDeployment `json:"captorDeployment,omitempty" yaml:"captorDeployment,omitempty"`

	// Match define what Kubernetes resources to apply this trap to.
	// Matching criteria are resources labels and/or namespaces.
	// +optional
	MatchResources MatchResources `json:"match,omitempty" yaml:"match,omitempty"`
}

// TrapType returns the type of trap.
func (trap *Trap) TrapType() TrapType {
	switch {
	case trap.FilesystemHoneytoken != FilesystemHoneytoken{}:
		return FilesystemHoneytokenTrap
	case trap.HttpEndpoint != HttpEndpoint{}:
		return HttpEndpointTrap
	case trap.HttpPayload != HttpPayload{}:
		return HttpPayloadTrap
	default:
		return UnknownTrap
	}
}

// IsValid checks if the trap specification is valid.
// The MatchResources field must include at least one of the MatchResources.Any.Namespaces or MatchResources.Any.Selector.
// Also, each individual trap will be validated as well. Note that only one trap can be specified at a time.
func (trap *Trap) IsValid() error {
	if trap.MatchResources.Any == nil {
		return errors.New("MatchResources.Any is nil")
	}

	for _, value := range trap.MatchResources.Any {
		if value.Namespaces == nil && value.Selector == nil {
			return errors.New("MatchResources.Any.Namespaces and MatchResources.Any.Selector are nil")
		}

		if len(value.Namespaces) == 0 && len(value.Selector.MatchLabels) == 0 {
			return errors.New("MatchResources.Any.Namespaces and MatchResources.Any.Selector are empty")
		}

		_, err := regexp.Compile(value.ContainerSelector)
		if err != nil {
			return fmt.Errorf("MatchResources.Any.ContainerSelector is not a valid regex: %w", err)
		}
	}

	numTraps := 0
	if (trap.FilesystemHoneytoken != FilesystemHoneytoken{}) {
		numTraps += 1
	}
	if (trap.HttpEndpoint != HttpEndpoint{}) {
		numTraps += 1
	}
	if (trap.HttpPayload != HttpPayload{}) {
		numTraps += 1
	}

	if numTraps != 1 {
		return fmt.Errorf("only one trap can be specified per list item, but %d traps were found", numTraps)
	}

	switch trap.TrapType() {
	case FilesystemHoneytokenTrap:
		if err := trap.FilesystemHoneytoken.IsValid(); err != nil {
			return err
		}
	case HttpEndpointTrap:
		if err := trap.HttpEndpoint.IsValid(); err != nil {
			return err
		}
	case HttpPayloadTrap:
		if err := trap.HttpPayload.IsValid(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("trap type is %T is unknown", trap)
	}

	return nil
}
