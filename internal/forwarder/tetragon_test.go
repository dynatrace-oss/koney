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
	"fmt"
	"strings"
	"time"

	ciliumiov1alpha1 "github.com/cilium/tetragon/pkg/k8s/apis/cilium.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/dynatrace-oss/koney/internal/controller/constants"
	"github.com/dynatrace-oss/koney/internal/controller/utils"
)

func tetragonLogLine(policyName, timestamp, arguments string) string {
	return fmt.Sprintf(`{"process_kprobe":{"policy_name":%q,"function_name":"security_file_permission",`+
		`"process":{"uid":0,"pid":42,"cwd":"/","binary":"/usr/bin/cat","arguments":%q,`+
		`"pod":{"name":"victim","namespace":"demo","container":{"id":"containerd://abc","name":"app"}}},`+
		`"args":[{"file_arg":{"path":"/run/secrets/koney/token"}}]},`+
		`"node_name":"node-1","time":%q}`,
		policyName, arguments, timestamp)
}

var _ = Describe("Tetragon events", func() {
	var forwarder *Forwarder

	BeforeEach(func() {
		tracingPolicy := &ciliumiov1alpha1.TracingPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "koney-tracing-policy-1",
				Labels: map[string]string{constants.LabelKeyDeceptionPolicyRef: "sample-policy"},
				Annotations: map[string]string{
					constants.AnnotationKeyContainerSelectors: `["regex:^app$"]`,
				},
			},
		}

		forwarder = New(Options{
			Client: fake.NewClientBuilder().WithScheme(testScheme).WithObjects(tracingPolicy).Build(),
		})
	})

	It("should collect the events of Koney tracing policies", func() {
		logs := strings.Join([]string{
			tetragonLogLine("koney-tracing-policy-1", "2025-08-12T10:11:12.000000000Z", ""),
			tetragonLogLine("some-other-policy", "2025-08-12T10:11:12.000000000Z", ""),
			"not json at all",
		}, "\n")

		events := map[string][]tetragonEvent{}
		forwarder.collectEvents(context.Background(), "tetragon-pod", logs, events)

		Expect(events).To(HaveLen(1))
		Expect(events["koney-tracing-policy-1"]).To(HaveLen(1))
	})

	It("should skip events that only differ in their sub-second time", func() {
		logs := strings.Join([]string{
			tetragonLogLine("koney-tracing-policy-1", "2025-08-12T10:11:12.000000000Z", ""),
			tetragonLogLine("koney-tracing-policy-1", "2025-08-12T10:11:12.999999999Z", ""),
			tetragonLogLine("koney-tracing-policy-1", "2025-08-12T10:11:13.000000000Z", ""),
		}, "\n")

		events := map[string][]tetragonEvent{}
		forwarder.collectEvents(context.Background(), "tetragon-pod", logs, events)

		Expect(events["koney-tracing-policy-1"]).To(HaveLen(2))
	})

	It("should skip events that were already collected", func() {
		logs := tetragonLogLine("koney-tracing-policy-1", "2025-08-12T10:11:12.000000000Z", "")

		events := map[string][]tetragonEvent{}
		forwarder.collectEvents(context.Background(), "tetragon-pod", logs, events)
		forwarder.collectEvents(context.Background(), "tetragon-pod", logs, events)

		Expect(events["koney-tracing-policy-1"]).To(HaveLen(1))
	})

	It("should map an event to an alert", func() {
		logs := tetragonLogLine("koney-tracing-policy-1", "2025-08-12T10:11:12.000000000Z", "--flag")

		events := map[string][]tetragonEvent{}
		forwarder.collectEvents(context.Background(), "tetragon-pod", logs, events)
		Expect(events["koney-tracing-policy-1"]).To(HaveLen(1))

		alert := forwarder.mapTetragonEvent(context.Background(), events["koney-tracing-policy-1"][0])

		Expect(alert.Timestamp).To(Equal("2025-08-12T10:11:12Z"))
		Expect(alert.TrapType).To(Equal(TrapTypeFilesystemHoneytoken))
		Expect(alert.Metadata).To(HaveKeyWithValue("file_path", "/run/secrets/koney/token"))
		Expect(alert.Pod.Name).To(Equal("victim"))
		Expect(alert.Pod.Container.ID).To(Equal("abc"))
		Expect(alert.Node.Name).To(Equal("node-1"))
		Expect(alert.Process.Binary).To(Equal("/usr/bin/cat"))
		Expect(alert.DeceptionPolicyName).To(HaveValue(Equal("sample-policy")))
	})

	It("should keep the fields that Tetragon omits as unknown", func() {
		logs := strings.Replace(tetragonLogLine("koney-tracing-policy-unknown", "2025-08-12T10:11:12Z", "x"), `"uid":0,`, "", 1)

		events := map[string][]tetragonEvent{}
		forwarder.collectEvents(context.Background(), "tetragon-pod", logs, events)
		alert := forwarder.mapTetragonEvent(context.Background(), events["koney-tracing-policy-unknown"][0])

		Expect(alert.Process.UID).To(BeNil())
		Expect(alert.Process.PID).To(HaveValue(Equal(42)))
		Expect(alert.DeceptionPolicyName).To(BeNil())
		Expect(MapToDynatraceEvent(alert, "LOW", "")["process.uid"]).To(BeNil())
	})

	It("should read the container selectors of a tracing policy", func() {
		ctx := context.Background()

		Expect(forwarder.resolveContainerSelectors(ctx, "koney-tracing-policy-1")).To(Equal([]string{"regex:^app$"}))
		Expect(forwarder.resolveContainerSelectors(ctx, "koney-tracing-policy-unknown")).To(BeNil())
	})

	It("should skip the alerts that Koney triggers itself", func() {
		fingerprinted := KoneyAlert{Process: &ProcessMetadata{
			Arguments: "-e foo\\c " + utils.EncodeFingerprintInEcho(utils.KoneyFingerprint),
		}}
		Expect(isOwnAlert(fingerprinted)).To(BeTrue())

		catted := KoneyAlert{Process: &ProcessMetadata{
			Arguments: utils.EncodeFingerprintInCat(utils.KoneyFingerprint) + " /run/secrets/koney/token",
		}}
		Expect(isOwnAlert(catted)).To(BeTrue())

		Expect(isOwnAlert(KoneyAlert{Process: &ProcessMetadata{Arguments: "/run/secrets/koney/token"}})).To(BeFalse())
		Expect(isOwnAlert(KoneyAlert{})).To(BeFalse())
	})

	It("should match container names against the selectors of a trap", func() {
		Expect(containerMatchesSelectors("app", []string{"app"})).To(BeTrue())
		Expect(containerMatchesSelectors("app", []string{"other", "regex:^ap"})).To(BeTrue())
		Expect(containerMatchesSelectors("app", []string{"glob:a*"})).To(BeTrue())
		Expect(containerMatchesSelectors("app", []string{""})).To(BeTrue())
		Expect(containerMatchesSelectors("app", []string{"other"})).To(BeFalse())
		Expect(containerMatchesSelectors("app", []string{"regex:("})).To(BeFalse())
		Expect(containerMatchesSelectors("", []string{""})).To(BeFalse())
	})

	It("should read the time that passed since the previous pull", func() {
		Expect(forwarder.logWindow()).To(BeEquivalentTo(minLogWindowSeconds))

		forwarder.lastReadAt = time.Now().Add(-5 * time.Minute)
		Expect(forwarder.logWindow()).To(BeEquivalentTo(300 + logWindowSlackSeconds))

		forwarder.lastReadAt = time.Now().Add(-10 * time.Hour)
		Expect(forwarder.logWindow()).To(BeEquivalentTo(maxLogWindowSeconds))

		forwarder.lastReadAt = time.Now()
		Expect(forwarder.logWindow()).To(BeEquivalentTo(minLogWindowSeconds))
	})

	It("should forget the oldest events when the cache is full", func() {
		cache := newEventCache(2)

		Expect(cache.seen("a")).To(BeFalse())
		Expect(cache.seen("b")).To(BeFalse())
		Expect(cache.seen("a")).To(BeTrue())

		Expect(cache.seen("c")).To(BeFalse()) // evicts "a"
		Expect(cache.seen("a")).To(BeFalse())
	})
})
