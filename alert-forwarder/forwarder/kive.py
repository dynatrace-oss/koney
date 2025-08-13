# Copyright (c) 2025 Dynatrace LLC
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU Affero General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU Affero General Public License for more details.
#
# You should have received a copy of the GNU Affero General Public License
# along with this program.  If not, see <http://www.gnu.org/licenses/>.

from .tetragon import _normalize_container_id
from .types import *


def process_kive_alert(kiveAlert: dict) -> KoneyAlert:
    koneyAlert = KoneyAlert(
        timestamp=kiveAlert["timestamp"],
        deception_policy_name=kiveAlert["custom-metadata"][
            "koney-deception-policy-name"
        ],
        trap_type="filesystem_honeytoken",
        metadata={
            "file_path": kiveAlert["metadata"]["path"],
        },
        pod=PodMetadata(
            name=kiveAlert["pod"]["name"],
            namespace=kiveAlert["pod"]["namespace"],
            container=ContainerMetadata(
                id=_normalize_container_id(kiveAlert["pod"]["container"]["id"]),
                name=kiveAlert["pod"]["container"]["name"],
            ),
        ),
        node=NodeMetadata(
            name=kiveAlert["node"]["name"],
        ),
        process=ProcessMetadata(
            uid=kiveAlert["process"]["uid"],
            pid=kiveAlert["process"]["pid"],
            cwd=kiveAlert["process"]["cwd"],
            binary=kiveAlert["process"]["binary"],
            arguments=kiveAlert["process"]["arguments"],
        ),
    )
    return koneyAlert
