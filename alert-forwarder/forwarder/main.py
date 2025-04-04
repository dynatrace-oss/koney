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
import logging
import time

from fastapi import BackgroundTasks, FastAPI, Response, status
from kubernetes import config
from rich.console import Console

from .tetragon import is_filtered_event, map_tetragon_event, read_tetragon_events

# various error messages
K8S_AUTH_ERROR = "failed to authenticate with Kubernetes API"

# the delay after receiving a (possibly multiple) triggers until we start loading alerts (once)
DEBOUNCE_SECONDS = 5

app = FastAPI(docs_url=None, redoc_url=None, openapi_url=None)
logger = logging.getLogger("uvicorn.error")
console = Console()

# global variable to remember when any handler was last triggered
most_recent_trigger = 0


@app.get("/handlers/tetragon", status_code=status.HTTP_202_ACCEPTED)
def handle_tetragon(response: Response, background_tasks: BackgroundTasks):
    global most_recent_trigger
    trigger_time = time.time()

    if not authenticate_kubernetes():
        response.status_code = status.HTTP_401_UNAUTHORIZED
        return dict(message=K8S_AUTH_ERROR)

    # enqueue a background task to load new alerts,
    # which will be debounced automatically
    most_recent_trigger = trigger_time
    background_tasks.add_task(load_new_alerts, timestamp=trigger_time)


def load_new_alerts(timestamp: float):
    global most_recent_trigger
    time.sleep(DEBOUNCE_SECONDS)
    if timestamp < most_recent_trigger:
        return  # another trigger was received in the meantime

    # TODO: if we are spammed with triggers, we never ever execute this code, fix that

    events_per_policy = read_tetragon_events()

    if not events_per_policy:
        return

    for policy_name, events in events_per_policy.items():
        if logger.level <= logging.DEBUG:
            console.print(f"Transforming {len(events)} alerts for policy {policy_name}")

        for event in events:
            koney_event = map_tetragon_event(event)
            if is_filtered_event(koney_event):
                if logger.level <= logging.DEBUG:
                    console.print(f"Skipping event ", koney_event)
                continue

            koney_event_str = json.dumps(koney_event)
            console.print(koney_event_str, soft_wrap=True)


@app.get("/healthz", status_code=status.HTTP_204_NO_CONTENT)
def readyz(response: Response):
    if not authenticate_kubernetes():
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE
        return dict(message=K8S_AUTH_ERROR)
    return None


def authenticate_kubernetes() -> bool:
    try:
        config.load_incluster_config()
        return True
    except config.config_exception.ConfigException:
        if logger.level <= logging.ERROR:
            console.print(K8S_AUTH_ERROR, style="bold red")
            console.print_exception()
        return False
