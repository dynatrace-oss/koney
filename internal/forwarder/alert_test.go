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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var sampleAlert = KoneyAlert{
	Timestamp:           "2025-08-12T10:11:12Z",
	DeceptionPolicyName: ptr("sample-policy"),
	TrapType:            TrapTypeFilesystemHoneytoken,
	Metadata:            map[string]any{"file_path": "/run/secrets/koney/token"},
	Pod: &PodMetadata{
		Name:      "victim",
		Namespace: "demo",
		Container: ContainerMetadata{ID: "abc", Name: "app"},
	},
	Node:    &NodeMetadata{Name: "node-1"},
	Process: &ProcessMetadata{UID: ptr(0), PID: ptr(42), Cwd: ptr("/"), Binary: ptr("/usr/bin/cat"), Arguments: ptr("/run/secrets/koney/token")},
}

var _ = Describe("Koney alerts", func() {
	It("should describe an access to a honeytoken", func() {
		Expect(sampleAlert.Description()).To(Equal(
			"Access to honeytoken (/run/secrets/koney/token) in pod (demo/victim) detected"))
	})

	It("should describe an alert of an unknown trap", func() {
		Expect(KoneyAlert{TrapType: TrapTypeUnknown}.Description()).To(Equal("Koney alert triggered"))
	})

	It("should fall back to placeholders when the metadata is missing", func() {
		alert := KoneyAlert{TrapType: TrapTypeFilesystemHoneytoken}
		Expect(alert.Description()).To(Equal("Access to honeytoken (?) in pod (?) detected"))
	})

	It("should give identical alerts the same id", func() {
		other := sampleAlert
		Expect(other.AlertID()).To(Equal(sampleAlert.AlertID()))

		other.Timestamp = "2025-08-12T10:11:13Z"
		Expect(other.AlertID()).NotTo(Equal(sampleAlert.AlertID()))
	})

	It("should map an alert to a Dynatrace event", func() {
		event := MapToDynatraceEvent(sampleAlert, "HIGH", "cluster-uid")

		Expect(event["timestamp"]).To(Equal(sampleAlert.Timestamp))
		Expect(event["event.kind"]).To(Equal("SECURITY_EVENT"))
		Expect(event["event.id"]).To(Equal(sampleAlert.AlertID()))
		Expect(event["finding.severity"]).To(Equal("HIGH"))
		Expect(event["dt.security.risk.score"]).To(Equal(8.9))
		Expect(event["k8s.cluster.uid"]).To(Equal("cluster-uid"))
		Expect(event["k8s.namespace.name"]).To(Equal("demo"))
		Expect(event["k8s.container.id"]).To(Equal("abc"))
		Expect(event["koney.metadata.file_path"]).To(Equal("/run/secrets/koney/token"))
		Expect(event["process.executable.name"]).To(Equal("cat"))
		Expect(event["process.executable.path"]).To(Equal("/usr/bin"))
		Expect(event["process.pid"]).To(Equal(42))
		Expect(event["koney.deception_policy_name"]).To(Equal("sample-policy"))
	})

	It("should map an alert without pod, node and process metadata", func() {
		event := MapToDynatraceEvent(KoneyAlert{TrapType: TrapTypeUnknown}, "LOW", "")

		Expect(event["k8s.pod.name"]).To(BeEmpty())
		Expect(event["k8s.node.name"]).To(BeEmpty())
		Expect(event["process.executable.name"]).To(BeNil())
		Expect(event["process.executable.path"]).To(BeNil())
		Expect(event["process.executable.arguments"]).To(BeNil())
		Expect(event["process.cwd"]).To(BeNil())
		Expect(event["k8s.cluster.uid"]).To(BeNil())
		Expect(event["dt.security.risk.score"]).To(Equal(3.9))
	})
})
