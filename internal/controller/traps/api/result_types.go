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

package api

import "github.com/dynatrace-oss/koney/api/v1alpha1"

type TrapDeploymentResult interface {
	// Trap referenes the trap that was deployed.
	GetTrap() *v1alpha1.Trap
	// Errors may contain one or more errors that happened during the deployment.
	GetErrors() error
	// ImplySuccess returns true if a deployment happened, it was successful, and we should not retry.
	ImpliesSuccess() bool
	// ImpliesFailure returns true if a deployment happened, but errors occurred.
	ImpliesFailure() bool
	// ImpliesRetry returns true if a we should retry the deployment later.
	ImpliesRetry() bool
}

type DecoyDeploymentResult struct {
	// Trap referenes the trap that was deployed.
	Trap *v1alpha1.Trap
	// AtLeastOneObjectsWasMatched indicates if we found at least one object in the cluster to which the trap should be deployed to.
	// Matched means that a object matches the trap's selector criteria (regardless of the resource's readiness).
	// One exception applies: Resources with a deletion timestamp are not even considered for matching.
	AtLeastOneObjectsWasMatched bool
	// AllObjectsWereReady indicates if all the objects that we wanted to deploy the trap to were ready.
	// If not, the deployment should be retried later. This can happen e.g., if containers are not running yet.
	// If no resources were matched or if errors occurred, this field should be ignored.
	AllObjectsWereReady bool
	// Errors may contain one or more errors that happened during the deployment.
	Errors error
}

func (result DecoyDeploymentResult) GetTrap() *v1alpha1.Trap {
	return result.Trap
}

func (result DecoyDeploymentResult) GetErrors() error {
	return result.Errors
}

func (result DecoyDeploymentResult) ImpliesSuccess() bool {
	return result.AtLeastOneObjectsWasMatched && result.AllObjectsWereReady && result.Errors == nil
}

func (result DecoyDeploymentResult) ImpliesFailure() bool {
	return result.Errors != nil
}

func (result DecoyDeploymentResult) ImpliesRetry() bool {
	return result.AtLeastOneObjectsWasMatched && !result.AllObjectsWereReady && result.Errors == nil
}

type CaptorDeploymentResult struct {
	// Trap referenes the trap that was deployed.
	Trap *v1alpha1.Trap
	// Errors may contain one or more errors that happened during the deployment.
	Errors error
	// MissingTetragon is set if we saw indications that Tetragon is not available in the cluster.
	MissingTetragon bool
	// UnsupportedSelectors is set alongside Errors when the captor cannot watch all resources
	// that the trap selects, so the status can show a specific reason.
	UnsupportedSelectors bool
}

func (result CaptorDeploymentResult) GetTrap() *v1alpha1.Trap {
	return result.Trap
}

func (result CaptorDeploymentResult) GetErrors() error {
	return result.Errors
}

func (result CaptorDeploymentResult) ImpliesSuccess() bool {
	return result.Errors == nil
}

func (result CaptorDeploymentResult) ImpliesFailure() bool {
	return result.Errors != nil || result.MissingTetragon
}

func (result CaptorDeploymentResult) ImpliesRetry() bool {
	return false
}
