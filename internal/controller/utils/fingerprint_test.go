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

package utils

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Fingerprint", func() {
	It("should derive the same fingerprint from the same token", func() {
		Expect(fingerprintOf("s3cret")).To(Equal(fingerprintOf("s3cret")))
		Expect(fingerprintOf("s3cret")).NotTo(Equal(fingerprintOf("another-token")))
	})

	It("should give every fingerprint the same length", func() {
		for _, token := range []string{"", "s3cret", "another-token", "yet-another-one"} {
			fingerprint := fingerprintOf(token)
			Expect(fingerprint).To(And(BeNumerically(">=", 1<<31), BeNumerically("<", 1<<32)))
			Expect(strings.Count(EncodeFingerprintInCat(fingerprint), "-u")).To(Equal(32))
			Expect(EncodeFingerprintInEcho(fingerprint)).To(HaveLen(len("KONEY_FINGERPRINT_") + 10))
		}
	})

	It("should not treat a plain cat -u as a fingerprint", func() {
		for _, token := range []string{"", "s3cret", "another-token"} {
			Expect("cat -u /run/secrets/token").NotTo(ContainSubstring(EncodeFingerprintInCat(fingerprintOf(token))))
		}
	})

	It("should encode a fingerprint in echo and cat calls", func() {
		Expect(EncodeFingerprintInEcho(1337)).To(Equal("KONEY_FINGERPRINT_1337"))
		Expect(EncodeFingerprintInCat(5)).To(Equal("-uu -u -uu"))
	})
})
