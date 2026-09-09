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

import hmac
import json
import logging
import os
import time

from fastapi import BackgroundTasks, FastAPI, Request, Response, status
from kubernetes import config
from rich.console import Console

from .kive import process_kive_alert
from .sink import K8S_SINK_READ_ERROR, SINK_SEND_ERROR, send_alert, try_read_alert_sinks
from .tetragon import (
    container_matches_selectors,
    is_filtered_alert,
    map_tetragon_event,
    read_tetragon_events,
    resolve_container_selectors,
)

# various error messages
K8S_AUTH_ERROR = "failed to authenticate with Kubernetes API"
WEBHOOK_AUTH_ERROR = "failed to authenticate the webhook caller"
WEBHOOK_TOKEN_MISSING = (
    "no webhook token configured, anyone can trigger the alert webhooks"
)

# the environment variable that holds the token which the controller adds
# to the webhook URLs that it hands out to Tetragon and Kive
WEBHOOK_TOKEN_ENV = "KONEY_ALERT_WEBHOOK_TOKEN"

# the delay after receiving a (possibly multiple) triggers until we start loading alerts (once)
DEBOUNCE_SECONDS = 5

app = FastAPI(docs_url=None, redoc_url=None, openapi_url=None)
logger = logging.getLogger("uvicorn.error")
console = Console()

# global variable to remember when any handler was last triggered
most_recent_trigger = 0


@app.get("/handlers/tetragon", status_code=status.HTTP_202_ACCEPTED)
def handle_tetragon(
    response: Response, request: Request, background_tasks: BackgroundTasks
):
    global most_recent_trigger
    trigger_time = time.time()

    if not authenticate_webhook(request):
        response.status_code = status.HTTP_401_UNAUTHORIZED
        return dict(message=WEBHOOK_AUTH_ERROR)

    if not authenticate_kubernetes():
        response.status_code = status.HTTP_401_UNAUTHORIZED
        return dict(message=K8S_AUTH_ERROR)

    # enqueue a background task to load new alerts,
    # which will be debounced automatically
    most_recent_trigger = trigger_time
    background_tasks.add_task(load_new_alerts, timestamp=trigger_time)


@app.post("/handlers/kive", status_code=status.HTTP_202_ACCEPTED)
async def handle_kive(response: Response, request: Request):
    if not authenticate_webhook(request):
        response.status_code = status.HTTP_401_UNAUTHORIZED
        return dict(message=WEBHOOK_AUTH_ERROR)

    if not authenticate_kubernetes():
        response.status_code = status.HTTP_401_UNAUTHORIZED
        return dict(message=K8S_AUTH_ERROR)

    koney_alert = process_kive_alert(await request.json())
    koney_alert_str = json.dumps(koney_alert)
    console.print(koney_alert_str, soft_wrap=True)

    alert_sinks = try_read_alert_sinks()

    # send to external systems
    for sink in alert_sinks:
        try:
            send_alert(koney_alert, sink)
        except:
            if logger.level <= logging.ERROR:
                console.print(SINK_SEND_ERROR, style="bold red")
                console.print_exception()


def load_new_alerts(timestamp: float):
    global most_recent_trigger
    time.sleep(DEBOUNCE_SECONDS)
    if timestamp < most_recent_trigger:
        return  # another trigger was received in the meantime

    # TODO (#29): if we are spammed with triggers, we never ever execute this code, fix that

    # resolve tetragon events
    events_per_policy = read_tetragon_events()
    if not events_per_policy:
        return

    alert_sinks = try_read_alert_sinks()

    # iterate over Tetragon events, map, log, and send alerts
    for policy_name, events in events_per_policy.items():
        if logger.level <= logging.DEBUG:
            console.print(f"Transforming {len(events)} alerts for policy {policy_name}")

        # resolve container selectors once per policy for client-side filtering (if needed)
        container_selectors = resolve_container_selectors(policy_name)

        for event in events:
            koney_alert = map_tetragon_event(event)
            if is_filtered_alert(koney_alert):
                if logger.level <= logging.DEBUG:
                    console.print("Skipping event (filtered) ", koney_alert)
                continue

            # filter by container selector when Tetragon matched all containers due to wildcards.
            if container_selectors is not None:
                container_name = (
                    (koney_alert.get("pod") or {}).get("container", {}).get("name")
                )
                if not container_matches_selectors(container_name, container_selectors):
                    if logger.level <= logging.DEBUG:
                        console.print("Skipping event (container filter) ", koney_alert)
                    continue

            # write to stdout
            koney_alert_str = json.dumps(koney_alert)
            console.print(koney_alert_str, soft_wrap=True)

            # send to external systems
            for sink in alert_sinks:
                try:
                    send_alert(koney_alert, sink)
                except:
                    if logger.level <= logging.ERROR:
                        console.print(SINK_SEND_ERROR, style="bold red")
                        console.print_exception()


@app.get("/healthz", status_code=status.HTTP_204_NO_CONTENT)
def readyz(response: Response):
    if not authenticate_kubernetes():
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE
        return dict(message=K8S_AUTH_ERROR)
    return None


def authenticate_webhook(request: Request) -> bool:
    """Checks that the caller knows the token that Koney puts into the webhook URLs.
    Installations that do not configure a token accept any caller, as before."""
    expected_token = os.environ.get(WEBHOOK_TOKEN_ENV, "")
    if not expected_token:
        if logger.level <= logging.WARNING:
            console.print(WEBHOOK_TOKEN_MISSING, style="bold yellow")
        return True

    return hmac.compare_digest(request.query_params.get("token", ""), expected_token)


def authenticate_kubernetes() -> bool:
    try:
        config.load_incluster_config()
        return True
    except config.config_exception.ConfigException:
        if logger.level <= logging.ERROR:
            console.print(K8S_AUTH_ERROR, style="bold red")
            console.print_exception()
        return False
