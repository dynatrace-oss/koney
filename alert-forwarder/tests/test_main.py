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

import pytest
from fastapi.testclient import TestClient
from forwarder import main

TOKEN = "s3cret"


@pytest.fixture
def client(monkeypatch):
    # the handlers must never reach the Kubernetes API or any alert sink in these tests
    monkeypatch.setattr(main, "authenticate_kubernetes", lambda: True)
    monkeypatch.setattr(main, "load_new_alerts", lambda timestamp: None)
    monkeypatch.setattr(main, "process_kive_alert", lambda alert: alert)
    monkeypatch.setattr(main, "try_read_alert_sinks", lambda: [])
    return TestClient(main.app)


@pytest.mark.parametrize("query", ["", "?token=", "?token=wrong"])
def test_handlers_reject_callers_without_the_token(client, monkeypatch, query):
    monkeypatch.setenv(main.WEBHOOK_TOKEN_ENV, TOKEN)

    assert client.get(f"/handlers/tetragon{query}").status_code == 401
    assert client.post(f"/handlers/kive{query}", json={}).status_code == 401


def test_handlers_accept_callers_with_the_token(client, monkeypatch):
    monkeypatch.setenv(main.WEBHOOK_TOKEN_ENV, TOKEN)

    assert client.get(f"/handlers/tetragon?token={TOKEN}").status_code == 202
    assert client.post(f"/handlers/kive?token={TOKEN}", json={}).status_code == 202


def test_handlers_accept_any_caller_without_a_configured_token(client, monkeypatch):
    monkeypatch.delenv(main.WEBHOOK_TOKEN_ENV, raising=False)

    assert client.get("/handlers/tetragon").status_code == 202
    assert client.post("/handlers/kive", json={}).status_code == 202


def test_health_check_is_not_authenticated(client, monkeypatch):
    monkeypatch.setenv(main.WEBHOOK_TOKEN_ENV, TOKEN)

    assert client.get("/healthz").status_code == 204
