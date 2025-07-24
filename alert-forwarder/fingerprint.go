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

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// TODO: Randomize on startup and sync with alerting system
const KoneyFingerprint = 1337

// Encodes the fingerprint for echo commands
func EncodeFingerprintInEcho(code int) string {
	return fmt.Sprintf("KONEY_FINGERPRINT_%d", code)
}

// Encodes the fingerprint for cat commands
func EncodeFingerprintInCat(code int) string {
	binaryCode := strconv.FormatInt(int64(code), 2)

	var builder strings.Builder

	for i, bit := range binaryCode {
		if i > 0 {
			builder.WriteByte(' ')
		}
		if bit == '0' {
			builder.WriteString("-u")
		} else {
			builder.WriteString("-uu")
		}
	}

	return builder.String()
}
