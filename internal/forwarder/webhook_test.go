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
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const webhookToken = "s3cret"

func newWebhookForwarder() *Forwarder {
	client := fake.NewClientBuilder().WithScheme(testScheme).Build()

	return New(Options{
		Client:    client,
		Clientset: k8sfake.NewClientset(),
		Namespace: sinkNamespace,
		Token:     webhookToken,
	})
}

func call(handler http.HandlerFunc, method, target string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler(recorder, request)

	return recorder
}

var _ = Describe("Alert webhooks", func() {
	var forwarder *Forwarder

	BeforeEach(func() {
		forwarder = newWebhookForwarder()
	})

	It("should reject callers that do not know the token", func() {
		for _, target := range []string{"/handlers/tetragon", "/handlers/tetragon?token=", "/handlers/tetragon?token=wrong"} {
			Expect(call(forwarder.handleTetragon(), http.MethodGet, target, "").Code).To(Equal(http.StatusUnauthorized))
		}

		recorder := call(forwarder.handleKive(), http.MethodPost, "/handlers/kive", kiveAlertBody)
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))

		Expect(forwarder.triggers).To(BeEmpty())
	})

	It("should take a trigger of Tetragon", func() {
		recorder := call(forwarder.handleTetragon(), http.MethodGet, "/handlers/tetragon?token="+webhookToken, "")

		Expect(recorder.Code).To(Equal(http.StatusAccepted))
		Expect(forwarder.triggers).To(HaveLen(1))
	})

	It("should collapse the triggers that arrive while a pull is pending", func() {
		for range 3 {
			recorder := call(forwarder.handleTetragon(), http.MethodGet, "/handlers/tetragon?token="+webhookToken, "")
			Expect(recorder.Code).To(Equal(http.StatusAccepted))
		}

		Expect(forwarder.triggers).To(HaveLen(1))
	})

	It("should take an alert of Kive", func() {
		recorder := call(forwarder.handleKive(), http.MethodPost,
			"/handlers/kive?token="+webhookToken, kiveAlertBody)

		Expect(recorder.Code).To(Equal(http.StatusAccepted))
		Expect(recorder.Header().Get("Content-Length")).To(Equal("0"))
		Expect(recorder.Flushed).To(BeTrue())
	})

	It("should reject an alert of Kive that is not valid json", func() {
		recorder := call(forwarder.handleKive(), http.MethodPost,
			"/handlers/kive?token="+webhookToken, "not json")

		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	})

	It("should reject an alert of Kive that misses fields", func() {
		recorder := call(forwarder.handleKive(), http.MethodPost,
			"/handlers/kive?token="+webhookToken, `{"timestamp": "2025-08-12T10:11:12Z"}`)

		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		Expect(recorder.Body.String()).To(ContainSubstring("pod.name"))
	})

	It("should reject an alert of Kive that is too large", func() {
		recorder := call(forwarder.handleKive(), http.MethodPost,
			"/handlers/kive?token="+webhookToken, strings.Repeat(" ", maxRequestSize+1))

		Expect(recorder.Code).To(Equal(http.StatusRequestEntityTooLarge))
	})
})

var _ = Describe("Serving", func() {
	It("should serve the webhooks on every replica and pull events on the leader only", func() {
		forwarder := newWebhookForwarder()

		Expect(forwarder.Webhooks()).To(BeAssignableToTypeOf(&webhookServer{}))
		Expect(forwarder.Webhooks().(*webhookServer).NeedLeaderElection()).To(BeFalse())
		Expect(forwarder.EventPuller().(*eventPuller).NeedLeaderElection()).To(BeTrue())
	})

	It("should fail to start when the address is taken", func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = listener.Close() }()

		forwarder := newWebhookForwarder()
		forwarder.Address = listener.Addr().String()

		Expect(forwarder.Webhooks().Start(context.Background())).NotTo(Succeed())
		Expect(forwarder.Ready(nil)).To(HaveOccurred())
	})

	It("should report readiness only while the webhooks are served", func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		address := listener.Addr().String()
		Expect(listener.Close()).To(Succeed())

		forwarder := newWebhookForwarder()
		forwarder.Address = address
		Expect(forwarder.Ready(nil)).To(HaveOccurred())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- forwarder.Webhooks().Start(ctx) }()

		Eventually(func() int {
			response, err := http.Get("http://" + address + "/handlers/tetragon?token=" + webhookToken)
			if err != nil {
				return 0
			}
			defer func() { _ = response.Body.Close() }()
			return response.StatusCode
		}).Within(5 * time.Second).Should(Equal(http.StatusAccepted))

		Expect(forwarder.Ready(nil)).To(Succeed())

		cancel()
		Eventually(done).Within(15 * time.Second).Should(Receive(BeNil()))
		Expect(forwarder.Ready(nil)).To(HaveOccurred())
	})
})

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
