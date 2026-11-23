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

import "os"

// GetKoneyNamespace retrieves the namespace where Koney is installed.
func GetKoneyNamespace() string {
	return GetEnv("KONEY_NAMESPACE", "koney-system")
}

// AlertWebhookTokenEnv is the environment variable that holds the token
// which authenticates the callers of the alert webhooks.
const AlertWebhookTokenEnv = "KONEY_ALERT_WEBHOOK_TOKEN"

// GetAlertWebhookToken retrieves the token that authenticates the callers of the alert webhooks.
func GetAlertWebhookToken() string {
	return GetEnv(AlertWebhookTokenEnv, "")
}

// AlertsSocketEnv is the environment variable that holds the path of the unix socket
// where the alerts container receives the alerts.
const AlertsSocketEnv = "KONEY_ALERTS_SOCKET"

// GetAlertsSocket retrieves the path of the unix socket where the alerts container receives the alerts.
func GetAlertsSocket() string {
	return GetEnv(AlertsSocketEnv, "/var/run/koney/alerts.sock")
}

// GetEnv retrieves the value of the environment variable named by the key.
// If the variable is present in the environment the value (which may be empty) is returned.
// Otherwise the fallback value is returned.
func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
