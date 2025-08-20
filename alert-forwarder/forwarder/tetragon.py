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

import json
import re
from collections import defaultdict
from typing import cast

from kubernetes import client

from .fingerprint import (
    KONEY_FINGERPRINT,
    encode_fingerprint_in_cat,
    encode_fingerprint_in_echo,
)
from .types import (
    ContainerMetadata,
    KoneyAlert,
    NodeMetadata,
    PodMetadata,
    ProcessMetadata,
)
from .utils import _normalize_container_id

# group, version, plural of the Tetragon TracingPolicy CRD
TETRAGON_TRACING_POLICIES_GVP = "cilium.io", "v1alpha1", "tracingpolicies"

# the namespace where Tetragon is assumed to be running
TETRAGON_NAMESPACE = "kube-system"
# all tracing policies created by Koney have this prefix
TETRAGON_POLICY_PREFIX = "koney-tracing-policy-"
# the label selector to find Tetragon pods
TETRAGON_POD_LABEL_SELECTOR = "app.kubernetes.io/name=tetragon"
# the container name where Tetragon logs are written
TETRAGON_POD_CONTAINER_NAME = "export-stdout"
# the label key that references the deception policy in a tracing policy
TETRAGON_DECEPTION_POLICY_REF = "koney/deception-policy"

# stores hashes of already processed events to prevent duplicates
event_cache = set()


def read_tetragon_events(since_seconds=60) -> dict[str, list[dict]]:
    v1 = client.CoreV1Api()

    pod_list = cast(
        client.V1PodList,
        v1.list_namespaced_pod(
            namespace=TETRAGON_NAMESPACE,
            label_selector=TETRAGON_POD_LABEL_SELECTOR,
        ),
    )

    if not pod_list.items:
        return {}  # no Tetragon pods found

    events_per_policy = defaultdict(list)
    for pod in pod_list.items:
        loglines = v1.read_namespaced_pod_log(
            name=pod.metadata.name,
            namespace=TETRAGON_NAMESPACE,
            container=TETRAGON_POD_CONTAINER_NAME,
            since_seconds=since_seconds,
        )

        for line in loglines.splitlines():
            # quickly filter-out lines that cannot match
            if TETRAGON_POLICY_PREFIX not in line:
                continue

            # events are often duplicated because kprobes can trigger multiple times.
            # as a simple de-duplication strategy, we remove the milliseconds from the timestamp.
            # this filters events that are completely identical and occurred within the same second.
            time_pattern = r'("time":"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})\.\d{9}(Z")'
            line = re.sub(time_pattern, r"\1\2", line)

            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                continue  # skip non-json lines in the logs

            # parse and check the referenced policy name
            if policy_name := _extract_tracing_policy_name(event):
                if not policy_name.startswith(TETRAGON_POLICY_PREFIX):
                    continue

                # avoid duplicates
                event_hash = hash(line)
                if event_hash in event_cache:
                    continue

                event_cache.add(event_hash)
                events_per_policy[policy_name].append(event)

    # returns the list of events (value) grouped by their policy name (key)
    return events_per_policy


def map_tetragon_event(event: dict) -> KoneyAlert:
    deception_policy_name = None
    trap_type = "unknown"
    metadata = dict()

    try:
        # attempt to resolve the DeceptionPolicy name (calls Kubernetes API)
        if tracing_policy_name := _extract_tracing_policy_name(event):
            deception_policy_name = _resolve_deception_policy_name(tracing_policy_name)
    except client.ApiException:
        pass

    # infer trap type and metadata by inspecting the event
    if kprobe := event.get("process_kprobe"):
        if meta := _extract_metadata_for_filesystem_honeytoken(kprobe):
            trap_type = "filesystem_honeytoken"
            metadata = meta

    pod = _extract_pod_metadata(event)
    node = _extract_node_metadata(event)
    process = _extract_process_metadata(event)

    # TODO: emit errors if we fail to resolve fields
    return KoneyAlert(
        timestamp=event["time"],
        deception_policy_name=deception_policy_name,
        trap_type=trap_type,
        metadata=metadata,
        pod=pod,
        node=node,
        process=process,
    )


def is_filtered_alert(alert: KoneyAlert) -> bool:
    if not alert["process"] or not alert["process"]["arguments"]:
        return False  # cannot decide, assume not filtered

    arguments = alert["process"]["arguments"]
    fingerprints = [
        encode_fingerprint_in_echo(KONEY_FINGERPRINT),
        encode_fingerprint_in_cat(KONEY_FINGERPRINT),
    ]

    # if any fingerprint is present, filter this event
    return any(fp in arguments for fp in fingerprints)


###############################################################################


def _resolve_deception_policy_name(tracing_policy_name: str) -> str | None:
    api = client.CustomObjectsApi()
    tracing_policy = cast(
        dict,
        api.get_cluster_custom_object(
            *TETRAGON_TRACING_POLICIES_GVP, tracing_policy_name
        ),
    )

    return (
        tracing_policy.get("metadata", {})
        .get("labels", {})
        .get(TETRAGON_DECEPTION_POLICY_REF)
    )


def _extract_tracing_policy_name(event: dict) -> str | None:
    # keys might be process_kprobe, process_uprobe, ...
    for value in event.values():
        if policy_name := value.get("policy_name"):
            return policy_name


def _extract_pod_metadata(event: dict) -> PodMetadata | None:
    # keys might be process_kprobe, process_uprobe, ...
    for value in event.values():
        if pod := value.get("process", {}).get("pod"):
            return PodMetadata(
                name=pod.get("name"),
                namespace=pod.get("namespace"),
                container=ContainerMetadata(
                    id=_normalize_container_id(pod.get("container", {}).get("id")),
                    name=pod.get("container", {}).get("name"),
                ),
            )


def _extract_node_metadata(event: dict) -> NodeMetadata | None:
    if node_name := event.get("node_name"):
        return NodeMetadata(name=node_name)
    return None


def _extract_process_metadata(event: dict) -> ProcessMetadata | None:
    # keys might be process_kprobe, process_uprobe, ...
    for value in event.values():
        if process := value.get("process"):
            return ProcessMetadata(
                uid=process.get("uid"),
                pid=process.get("pid"),
                cwd=process.get("cwd"),
                binary=process.get("binary"),
                arguments=process.get("arguments"),
            )


def _extract_metadata_for_filesystem_honeytoken(kprobe: dict) -> dict | None:
    file_access_fn = ("security_file_permission", "security_mmap_file")
    if kprobe.get("function_name") in file_access_fn:
        file_path = kprobe.get("args", [{}])[0].get("file_arg", {}).get("path")
        return dict(file_path=file_path)
