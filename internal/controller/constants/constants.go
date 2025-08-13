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

package constants

import (
	"time"
)

const (
	// AnnotationKeyChanges is the annotation key that is placed on resources that have been modified by Koney.
	// Koney needs this annotation when cleaning up or updating traps. Also, this makes it easier to see modified resources.
	AnnotationKeyChanges = "koney/changes"

	// FinalizerName is the name of the finalizer that Koney places on each DeceptionPolicy.
	// The presence of this finalizer means that traps still need to be cleaned up (e.g., when the DeceptionPolicy is deleted).
	FinalizerName = "koney/finalizer"

	// LabelKeyDeceptionPolicyRef is the label key that is placed on resources to indicate that they are managed by Koney.
	// Koney might create resources such as a TracingPolicy for captors.
	LabelKeyDeceptionPolicyRef = "koney/deception-policy"

	// The name used by our controller to claim ownership of fields when doing server-side apply in Kubernetes.
	FieldOwnerKoneyController = "koney-controller"

	// If reconciliation fails, retry after this interval.
	NormalFailureRetryInterval = 1 * time.Minute

	// If resources are not ready yet for traps (e.g., containers are still starting), retry reconciliation after this shorter interval.
	ShortStatusCheckInterval = 10 * time.Second

	// WildcardContainerSelectorRegex is a regex that matches wildcard characters in container selector fields.
	WildcardContainerSelectorRegex = `\*|\?|\[|\]`
)
