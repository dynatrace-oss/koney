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
	"context"
	"time"

	k8slog "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	// debounceDelay is the quiet period that a trigger waits for, so that a burst
	// of triggers collapses into a single pull of the Tetragon events.
	debounceDelay = 5 * time.Second

	// maxDebounceDelay is the longest that a pull is held back, so that a steady
	// stream of triggers cannot postpone it forever.
	maxDebounceDelay = 30 * time.Second
)

type eventPuller struct {
	*Forwarder
}

func (*eventPuller) NeedLeaderElection() bool {
	return true
}

// Start pulls the Tetragon events until the context is cancelled.
func (f *eventPuller) Start(ctx context.Context) error {
	log := k8slog.FromContext(ctx).WithName("forwarder")
	f.pullLoop(k8slog.IntoContext(ctx, log))

	return nil
}

// pullLoop pulls the Tetragon events once a burst of triggers has calmed down.
func (f *Forwarder) pullLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.triggers:
			if !f.waitForQuietPeriod(ctx) {
				return
			}
			f.loadNewAlerts(ctx)
		}
	}
}

// waitForQuietPeriod waits until no trigger was received for debounceDelay,
// but never longer than maxDebounceDelay. It reports whether the wait finished.
func (f *Forwarder) waitForQuietPeriod(ctx context.Context) bool {
	quiet := time.NewTimer(debounceDelay)
	defer quiet.Stop()
	deadline := time.NewTimer(maxDebounceDelay)
	defer deadline.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-f.triggers:
			quiet.Reset(debounceDelay)
		case <-quiet.C:
			return true
		case <-deadline.C:
			return true
		}
	}
}
