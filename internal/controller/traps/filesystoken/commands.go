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

import "strings"

// The dynamic values (file path, octal content and fingerprints) are passed to
// the shell as positional arguments instead of being interpolated into the
// script, so the shell never re-parses them as code. The file path in
// particular comes from the DeceptionPolicy and is attacker-controlled.

func fileExistenceCheckCommand(filePath string) []string {
	return []string{"sh", "-c", `[ ! -f "$1" ] && echo 'No such file' || echo 'File exists'`, "sh", filePath}
}

func writeOctalContentCommand(octalContent, echoFingerprint, filePath string) []string {
	return []string{
		"sh", "-c",
		`oct_string="$2"; i=1; while [ $i -lt ${#oct_string} ]; do $(which echo) -e "\0$(expr substr $oct_string $i 3)\c $3"; i=$(expr $i + 3); done > "$1"`,
		"sh", filePath, octalContent, echoFingerprint,
	}
}

func writeEmptyFileCommand(echoFingerprint, filePath string) []string {
	return []string{"sh", "-c", `echo -e "\c $2" > "$1"`, "sh", filePath, echoFingerprint}
}

func readFileContentCommand(catFingerprint, filePath string) []string {
	// catFingerprint is a space-joined list of `-u`/`-uu` flags; pass each as its
	// own positional arg so cat sees them as flags, with the file last, via "$@".
	args := []string{"sh", "-c", `cat "$@"`, "sh"}
	args = append(args, strings.Fields(catFingerprint)...)
	return append(args, filePath)
}
