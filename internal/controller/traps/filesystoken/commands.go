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

package filesystoken

import (
	"fmt"
	"strings"
)

// The dynamic values (file path, octal content and fingerprints) are passed to
// the shell as positional arguments instead of being interpolated into the
// script, so the shell never re-parses them as code. The file path in
// particular comes from the DeceptionPolicy and is attacker-controlled.

func fileExistenceCheckCommand(filePath string) []string {
	return []string{"sh", "-c", `[ ! -f "$1" ] && echo 'No such file' || echo 'File exists'`, "sh", filePath}
}

// writeFileContentCommand writes content to the file, which empties the file if content is empty.
// It uses printf, which every POSIX shell has (unlike `echo -e` or `which`), and passes
// the content as a format string in which octal escapes stand for all special bytes.
// `%.0s` adds the fingerprint to the arguments of printf without printing it,
// in case printf is not a shell built-in, such as in posh.
func writeFileContentCommand(content, printfFingerprint, filePath string) []string {
	return []string{"sh", "-c", `printf "$2%.0s" "$3" > "$1"`, "sh", filePath, printfFormat(content), printfFingerprint}
}

// printfFormat encodes content as a printf format string that prints exactly that content.
// Only ASCII letters and digits are kept as they are, so the format has no `%`, no `\` other than
// its own escapes, and cannot start with `-`. Escapes always have three octal digits, so a digit after them is a literal.
func printfFormat(content string) string {
	var b strings.Builder
	for _, c := range []byte(content) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "\\%03o", c)
		}
	}
	return b.String()
}

func readFileContentCommand(catFingerprint, filePath string) []string {
	// catFingerprint is a space-joined list of `-u`/`-uu` flags; pass each as its
	// own positional arg so cat sees them as flags, with the file last, via "$@".
	args := []string{"sh", "-c", `cat "$@"`, "sh"}
	args = append(args, strings.Fields(catFingerprint)...)
	return append(args, filePath)
}
