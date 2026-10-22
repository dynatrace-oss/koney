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
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
)

// fingerprintHighBit keeps every fingerprint at 32 bits, so that it never encodes as a short `cat -u`.
const fingerprintHighBit = 1 << 31

var (
	fingerprint     int
	fingerprintOnce sync.Once
)

// Fingerprint returns the fingerprint of this Koney installation. Koney marks the commands
// that it runs itself with it, so that it does not alert on its own trap deployments.
// It is derived from the webhook token, so that it is unpredictable for an attacker
// but stays the same across restarts.
func Fingerprint() int {
	fingerprintOnce.Do(func() {
		fingerprint = fingerprintOf(GetAlertWebhookToken())
	})

	return fingerprint
}

func fingerprintOf(token string) int {
	digest := sha256.Sum256([]byte("koney-fingerprint:" + token))

	return int(binary.BigEndian.Uint32(digest[:4]) | fingerprintHighBit)
}

// EncodeFingerprintInEcho encodes a fingerprint in a call to `echo`, to be
// used, e.g. in a call such as `echo -e "foobar\c KONEY_FINGERPRINT_123"` after
// the `\c` escape sequence. Everything after the `\c` escape sequence is
// ignored by `echo` when `-e` is used, thus, the command will still work as
// expected. This is useful to mark `echo` calls from Koney, so that we won't
// alert on them later.
func EncodeFingerprintInEcho(code int) string {
	return fmt.Sprintf("KONEY_FINGERPRINT_%d", code)
}

// EncodeFingerprintInCat encodes a fingerprint in a call to `cat`, to be used,
// e.g. in a call such as `cat -u -uu -u -u -uu /foo/bar` where the `-u` flag is
// used to binary-encode the fingerprint (`-u` is 0, `-uu` is 1). The `-u` flag
// is ignored by `cat`, thus, the command will still work as expected. This is
// useful to mark `cat` calls from Koney, so that we won't alert on them later.
func EncodeFingerprintInCat(code int) string {
	binaryCode := fmt.Sprintf("%b", code)

	// iterate and replace 0 with -u and 1 with -uu
	cipher := ""
	for _, bit := range binaryCode {
		if bit == '0' {
			cipher += "-u "
		} else {
			cipher += "-uu "
		}
	}

	// remove the trailing space
	cipher = cipher[:len(cipher)-1]

	return cipher
}
