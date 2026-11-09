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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const kiveAlertBody = `{
  "timestamp": "2025-08-12T10:11:12Z",
  "custom-metadata": {"koney-deception-policy-name": "sample-policy"},
  "metadata": {"path": "/run/secrets/koney/token"},
  "pod": {"name": "victim", "namespace": "demo", "container": {"id": "containerd://abc", "name": "app"}},
  "node": {"name": "node-1"},
  "process": {"uid": 0, "pid": 42, "cwd": "/", "binary": "/usr/bin/cat", "arguments": "/run/secrets/koney/token"}
}`

var _ = Describe("Kive alerts", func() {
	It("should map an alert of Kive to a Koney alert", func() {
		alert, err := mapKiveAlert([]byte(kiveAlertBody))
		Expect(err).NotTo(HaveOccurred())

		Expect(alert.Timestamp).To(Equal("2025-08-12T10:11:12Z"))
		Expect(alert.DeceptionPolicyName).To(HaveValue(Equal("sample-policy")))
		Expect(alert.TrapType).To(Equal(TrapTypeFilesystemHoneytoken))
		Expect(alert.Metadata).To(HaveKeyWithValue("file_path", "/run/secrets/koney/token"))
		Expect(alert.Pod.Container.ID).To(Equal("abc"))
		Expect(alert.Node.Name).To(Equal("node-1"))
		Expect(alert.Process.PID).To(HaveValue(Equal(42)))
	})

	It("should reject an alert that is not valid json", func() {
		_, err := mapKiveAlert([]byte("not json"))
		Expect(err).To(HaveOccurred())
	})

	It("should name the fields that an alert misses", func() {
		_, err := mapKiveAlert([]byte(`{"timestamp": "t", "pod": {"name": "victim"}, "process": null}`))
		Expect(err).To(MatchError(And(
			ContainSubstring("custom-metadata.koney-deception-policy-name"),
			ContainSubstring("pod.namespace"),
			ContainSubstring("process.pid"),
			Not(ContainSubstring("pod.name,")),
		)))
	})

	It("should reject an alert with unexpected types", func() {
		body := strings.Replace(kiveAlertBody, `"pid": 42`, `"pid": "42"`, 1)
		_, err := mapKiveAlert([]byte(body))
		Expect(err).To(HaveOccurred())
	})
})
