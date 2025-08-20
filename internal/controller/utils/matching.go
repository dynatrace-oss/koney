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

func MatchContainerName(pattern string, containerName string) (bool, error) {

	if pattern == "" {
		return true, nil
	}

	if strings.HasPrefix(pattern, "regex:") {

		compiledPattern, err := regexp.Compile(strings.TrimPrefix(pattern, "regex:"))
		if err != nil {
			return false, fmt.Errorf("error compiling regex: %w", err)
		}

		return compiledPattern.Match([]byte(containerName)), nil

	} else if strings.HasPrefix(pattern, "glob:") {

		result, err := filepath.Match(strings.TrimPrefix(pattern, "glob:"), containerName)
		if err != nil {
			return false, fmt.Errorf("error compiling glob expression: %w", err)
		}
		return result, nil
	}

	// Direct comparison
	return pattern == containerName, nil
}
