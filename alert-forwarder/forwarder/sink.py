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

import base64
import logging
import os
from functools import cache
from typing import cast

import requests
from kubernetes import client
from rich.console import Console

from .alerts import map_to_dynatrace_event
from .types import AlertSink, DynatraceSink, KoneyAlert

# the namespace where Koney and the DeceptionAlertSink CRDs are located
KONEY_NAMESPACE = os.environ.get("KONEY_NAMESPACE", "koney-system")

# group, version, plural of the Koney DeceptionAlertSink CRD
KONEY_DECEPTION_ALERT_SINK_GVNP = (
    "research.dynatrace.com",
    "v1alpha1",
    KONEY_NAMESPACE,
    "deceptionalertsinks",
)

# number of seconds after we timeout requests to external systems
SINK_REQUEST_TIMEOUT = 25

logger = logging.getLogger("uvicorn.error")
console = Console()


def read_alert_sinks() -> list[AlertSink]:
    api = client.CustomObjectsApi()
    objs = api.list_namespaced_custom_object(*KONEY_DECEPTION_ALERT_SINK_GVNP)

    alert_sinks = []
    for obj in objs.get("items", []):
        alert_sink = AlertSink(
            name=obj.get("metadata", {}).get("name"),
            dynatrace_sink=_extract_dynatrace_sink(obj),
        )
        alert_sinks.append(alert_sink)

    return alert_sinks


def send_alert(koney_alert: KoneyAlert, sink: AlertSink) -> None:
    cluster_uid = _get_cluster_uid()

    if sink["dynatrace_sink"]:
        api_url = sink["dynatrace_sink"]["api_url"]
        api_token = sink["dynatrace_sink"]["api_token"]
        severity = sink["dynatrace_sink"]["severity"]

        payload = map_to_dynatrace_event(koney_alert, severity, cluster_uid)
        if logger.level <= logging.DEBUG:
            console.print("Sending alert to Dynatrace:", payload)

        resp = requests.post(
            f"{api_url}/platform/ingest/v1/security.events",
            json=payload,
            timeout=SINK_REQUEST_TIMEOUT,
            headers={
                "Authorization": f"Api-Token {api_token}",
                "Content-Type": "application/json",
            },
        )

        # check response status
        if resp.status_code != 202:
            raise RuntimeError(
                f"failed to send alert to Dynatrace: {resp.status_code} {resp.text}"
            )


###############################################################################


def _extract_dynatrace_sink(obj: dict) -> DynatraceSink | None:
    if spec := obj.get("spec", {}).get("dynatrace"):
        if secret_name := spec.get("secretName"):
            if secret := _get_decoded_secret_data(secret_name):
                return DynatraceSink(
                    api_url=secret["apiUrl"],
                    api_token=secret["apiToken"],
                    severity=obj["spec"]["dynatrace"]["severity"],
                )


def _get_decoded_secret_data(secret_name: str) -> dict | None:
    api = client.CoreV1Api()
    secret = cast(
        client.V1Secret,
        api.read_namespaced_secret(secret_name, KONEY_NAMESPACE),
    )

    if not secret.data:
        return None  # empty secret

    # decode base64-encoded data
    decoded_data = {}
    for key, value in secret.data.items():
        decoded_data[key] = base64.b64decode(value).decode("utf-8")

    return decoded_data


@cache
def _get_cluster_uid() -> str | None:
    # get the uid of the kube-system namespace
    api = client.CoreV1Api()
    namespace = cast(client.V1Namespace, api.read_namespace("kube-system"))
    if not namespace.metadata or not namespace.metadata.uid:
        return None
    return namespace.metadata.uid
