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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandsDoNotInterpolateDynamicValues(t *testing.T) {
	const filePath = "/run/secrets/token"

	cases := []struct {
		name   string
		cmd    []string
		values []string
	}{
		{"existence", fileExistenceCheckCommand(filePath), []string{filePath}},
		{"write", writeFileContentCommand("abc", "fp", filePath), []string{"abc", "fp", filePath}},
		{"read", readFileContentCommand("-u -uu", filePath), []string{"-u", "-uu", filePath}},
	}

	for _, tc := range cases {
		if len(tc.cmd) < 5 || tc.cmd[0] != "sh" || tc.cmd[1] != "-c" || tc.cmd[3] != "sh" {
			t.Fatalf("%s: want an `sh -c <script> sh <args...>` form, got %v", tc.name, tc.cmd)
		}

		script, positional := tc.cmd[2], tc.cmd[4:]
		for _, v := range tc.values {
			if strings.Contains(script, v) {
				t.Errorf("%s: %q must not be interpolated into the script: %q", tc.name, v, script)
			}
			if !contains(positional, v) {
				t.Errorf("%s: %q must be passed as a positional argument, got %v", tc.name, v, positional)
			}
		}
	}
}

func TestDynamicValuesAreNotShellInterpreted(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	marker := filepath.Join(dir, "pwned")
	inject := "$(touch " + marker + ")"

	for _, cmd := range [][]string{
		fileExistenceCheckCommand(inject),
		writeFileContentCommand(inject, inject, out),
		readFileContentCommand(inject, inject),
	} {
		_ = exec.Command(cmd[0], cmd[1:]...).Run() // command errors are irrelevant; only injection matters

		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("a dynamic value was shell-interpreted: injected command ran for %v", cmd)
		}
	}
}

func TestWriteFileContentCommandWritesExactContent(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}

	for _, content := range []string{
		"someverysecrettoken",
		"", // must also empty a file that has content, see #86
		"-n",
		"-e foo",
		`% %s %% %b \ \c \0101 \n '"$(id) ` + "`id`; \n\n",
		"päßwörd 🔑\n",
		string(allBytes),
	} {
		if err := os.WriteFile(out, []byte("previous content"), 0o600); err != nil {
			t.Fatal(err)
		}

		cmd := writeFileContentCommand(content, "KONEY_FINGERPRINT_2147483649", out)
		if output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%q: command failed: %v: %s", content, err, output)
		}

		if got, _ := os.ReadFile(out); string(got) != content {
			t.Errorf("want %q, got %q", content, got)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
