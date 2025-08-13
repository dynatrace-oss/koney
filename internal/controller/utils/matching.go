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
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

func MatchContainerName(regex string, containerName string) (bool, error) {

	if regex == "" {
		return true, nil
	}

	if strings.HasPrefix(regex, "regex:") {

		compiledRegex, err := regexp.Compile(strings.TrimPrefix(regex, "regex:"))
		if err != nil {
			return false, fmt.Errorf("error compiling regex: %w", err)
		}

		return compiledRegex.Match([]byte(containerName)), nil

	} else if strings.HasPrefix(regex, "glob:") {

		result, err := filepath.Match(strings.TrimPrefix(regex, "glob:"), containerName)
		if err != nil {
			return false, fmt.Errorf("error compiling glob expression: %w", err)
		}
		return result, nil
	}

	// Direct comparison
	return regex == containerName, nil
}
