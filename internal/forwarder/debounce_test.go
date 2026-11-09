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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Debouncing", func() {
	var forwarder *Forwarder

	BeforeEach(func() {
		forwarder = newWebhookForwarder()

		quiet, max := debounceDelay, maxDebounceDelay
		debounceDelay, maxDebounceDelay = 50*time.Millisecond, 200*time.Millisecond
		DeferCleanup(func() { debounceDelay, maxDebounceDelay = quiet, max })
	})

	It("should wait for the quiet period after the last trigger", func() {
		started := time.Now()
		triggers := forwarder.triggers
		go func() {
			defer GinkgoRecover()
			for range 2 {
				time.Sleep(30 * time.Millisecond)
				triggers <- struct{}{}
			}
		}()

		Expect(forwarder.waitForQuietPeriod(context.Background())).To(BeTrue())
		Expect(time.Since(started)).To(BeNumerically(">=", 80*time.Millisecond))
	})

	It("should pull anyway when the triggers never stop", func() {
		ctx, cancel := context.WithCancel(context.Background())
		triggers := forwarder.triggers
		stopped := make(chan struct{})
		defer func() {
			cancel()
			<-stopped
		}()

		go func() {
			defer GinkgoRecover()
			defer close(stopped)
			for {
				select {
				case <-ctx.Done():
					return
				case triggers <- struct{}{}:
					time.Sleep(10 * time.Millisecond)
				}
			}
		}()

		started := time.Now()
		Expect(forwarder.waitForQuietPeriod(ctx)).To(BeTrue())
		Expect(time.Since(started)).To(BeNumerically("<", time.Second))
	})

	It("should stop waiting when the manager shuts down", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		Expect(forwarder.waitForQuietPeriod(ctx)).To(BeFalse())
	})
})
