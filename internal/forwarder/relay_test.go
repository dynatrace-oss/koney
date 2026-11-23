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
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr/funcr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	k8slog "sigs.k8s.io/controller-runtime/pkg/log"
)

// lockedBuffer is written by the relay and read by the test.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}

var _ = Describe("Alert relay", func() {
	var socket string
	var logs *lockedBuffer
	var ctx context.Context
	var forwarder *Forwarder

	BeforeEach(func() {
		// not GinkgoT().TempDir(), whose long paths exceed the limit of unix socket paths on macOS
		dir, err := os.MkdirTemp("", "koney")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(os.RemoveAll, dir)
		socket = filepath.Join(dir, "alerts.sock")

		logs = &lockedBuffer{}
		ctx = k8slog.IntoContext(context.Background(), funcr.New(func(prefix, args string) {
			_, _ = logs.Write([]byte(prefix + args + "\n"))
		}, funcr.Options{}))

		client := fake.NewClientBuilder().WithScheme(testScheme).Build()
		forwarder = New(Options{Client: client, Clientset: k8sfake.NewClientset(), Namespace: sinkNamespace,
			AlertsSocket: socket})
	})

	// startRelay runs the relay until the end of the test process, like the alerts container.
	startRelay := func() *lockedBuffer {
		out := &lockedBuffer{}
		go func() { _ = RelayAlerts(socket, out) }()
		// not just the socket file, which a previous relay may have left behind
		Eventually(func() error {
			conn, err := net.Dial("unixgram", socket)
			if err == nil {
				_ = conn.Close()
			}
			return err
		}).Should(Succeed())
		return out
	}

	It("should log every forwarded alert as one JSON line in the alerts container", func() {
		out := startRelay()

		second := sampleAlert
		second.Timestamp = "2025-08-12T10:11:13Z"
		forwarder.forwardAlert(ctx, sampleAlert, nil)
		forwarder.forwardAlert(ctx, second, nil)

		Eventually(out.String).Should(HaveSuffix("\n"))
		Eventually(func() []string { return strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") }).
			Should(HaveLen(2))
		lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
		for i, want := range []KoneyAlert{sampleAlert, second} {
			var got KoneyAlert
			Expect(json.Unmarshal([]byte(lines[i]), &got)).To(Succeed())
			Expect(got.Timestamp).To(Equal(want.Timestamp))
		}
		Expect(logs.String()).To(BeEmpty())
	})

	It("should not lose the first alert after the alerts container restarted", func() {
		// the previous relay, which the forwarder is connected to when it goes away
		previous, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socket, Net: "unixgram"})
		Expect(err).NotTo(HaveOccurred())
		forwarder.forwardAlert(ctx, sampleAlert, nil)
		buffer := make([]byte, relayBufferSize)
		n, err := previous.Read(buffer)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(buffer[:n])).To(ContainSubstring(sampleAlert.Timestamp))
		Expect(previous.Close()).To(Succeed())

		out := startRelay()
		forwarder.forwardAlert(ctx, sampleAlert, nil)
		Eventually(out.String).Should(ContainSubstring(sampleAlert.Timestamp))
		Expect(logs.String()).To(BeEmpty())
	})

	It("should not block but log the alert while the alerts container is down, and recover", func() {
		done := make(chan struct{})
		go func() {
			defer close(done)
			forwarder.forwardAlert(ctx, sampleAlert, nil)
		}()
		Eventually(done).WithTimeout(time.Second).Should(BeClosed())
		Expect(logs.String()).To(ContainSubstring("Unable to relay an alert to the alerts container"))
		Expect(logs.String()).To(ContainSubstring(sampleAlert.Timestamp))

		// a file left behind by a previous relay is replaced, and the forwarder dials again
		Expect(os.WriteFile(socket, nil, 0o600)).To(Succeed())
		out := startRelay()
		Eventually(func() string {
			forwarder.forwardAlert(ctx, sampleAlert, nil)
			return out.String()
		}).WithTimeout(5 * time.Second).Should(ContainSubstring(sampleAlert.Timestamp))
	})
})
