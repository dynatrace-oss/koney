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

package matching

import "sigs.k8s.io/controller-runtime/pkg/client"

func ContainerSelectorSelectsAll(containerSelector string) bool {
	return containerSelector == "regex:.*" || containerSelector == "" || containerSelector == "glob:*"
}

// extractObjectNames is a helper function that extracts the names of the objects from a list of objects.
func extractObjectNames(objects []client.Object) []string {
	names := make([]string, len(objects))
	for i, podOrDeployment := range objects {
		names[i] = podOrDeployment.GetName()
	}
	return names
}

// getObjectFromMap returns an object from a map of objects based on its name.
func getObjectFromMap(objectName string, objectMap map[client.Object][]string) client.Object {
	for object := range objectMap {
		if object.GetName() == objectName {
			return object
		}
	}

	return nil
}
