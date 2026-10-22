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

package forwarder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8slog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
)

const (
	// sinkRequestTimeout is the time after which requests to external systems are given up on.
	sinkRequestTimeout = 25 * time.Second

	// dynatraceIngestPath is the path of the Dynatrace API that takes security events.
	dynatraceIngestPath = "/platform/ingest/v1/security.events"
)

// alertSink is a resolved DeceptionAlertSink, with the credentials of its secret already read.
type alertSink struct {
	name      string
	dynatrace *dynatraceSink
}

type dynatraceSink struct {
	apiURL   string
	apiToken string
	severity string
}

// readAlertSinks resolves all DeceptionAlertSink resources of the Koney namespace.
// Sinks whose secret is missing or incomplete are skipped, the others are still returned.
func (f *Forwarder) readAlertSinks(ctx context.Context) []alertSink {
	log := k8slog.FromContext(ctx)

	sinkList := &v1alpha1.DeceptionAlertSinkList{}
	if err := f.Client.List(ctx, sinkList, client.InNamespace(f.Namespace)); err != nil {
		log.Error(err, "Unable to read DeceptionAlertSink resources")
		return nil
	}

	sinks := make([]alertSink, 0, len(sinkList.Items))
	for _, item := range sinkList.Items {
		sink := alertSink{name: item.Name}

		if item.Spec.Dynatrace.SecretName != "" {
			dynatrace, err := f.readDynatraceSink(ctx, item.Spec.Dynatrace)
			if err != nil {
				log.Error(err, "Unable to read the secret of a DeceptionAlertSink", "sink", item.Name)
				continue
			}
			sink.dynatrace = dynatrace
		}

		sinks = append(sinks, sink)
	}

	return sinks
}

func (f *Forwarder) readDynatraceSink(ctx context.Context, spec v1alpha1.DynatraceSinkSpec) (*dynatraceSink, error) {
	secret := &corev1.Secret{}
	key := client.ObjectKey{Name: spec.SecretName, Namespace: f.Namespace}
	if err := f.Client.Get(ctx, key, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("secret %q not found in namespace %q", spec.SecretName, f.Namespace)
		}
		return nil, err
	}

	apiURL, apiToken := string(secret.Data["apiUrl"]), string(secret.Data["apiToken"])
	if apiURL == "" || apiToken == "" {
		return nil, fmt.Errorf("secret %q does not hold apiUrl and apiToken", spec.SecretName)
	}

	return &dynatraceSink{apiURL: apiURL, apiToken: apiToken, severity: spec.Severity}, nil
}

// sendAlert sends an alert to one sink.
func (f *Forwarder) sendAlert(ctx context.Context, alert KoneyAlert, sink alertSink) error {
	if sink.dynatrace == nil {
		return nil
	}

	clusterUID, err := f.clusterUID(ctx)
	if err != nil {
		k8slog.FromContext(ctx).Error(err, "Unable to resolve the cluster uid")
	}

	payload, err := json.Marshal(MapToDynatraceEvent(alert, sink.dynatrace.severity, clusterUID))
	if err != nil {
		return err
	}

	requestCtx, cancel := context.WithTimeout(ctx, sinkRequestTimeout)
	defer cancel()

	url := sink.dynatrace.apiURL + dynatraceIngestPath
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Api-Token "+sink.dynatrace.apiToken)
	request.Header.Set("Content-Type", "application/json")

	response, err := f.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("failed to send alert to Dynatrace: %d %s", response.StatusCode, string(body))
	}

	return nil
}

// forwardAlert writes an alert to the log of the controller and sends it to all sinks.
func (f *Forwarder) forwardAlert(ctx context.Context, alert KoneyAlert, sinks []alertSink) {
	log := k8slog.FromContext(ctx)

	encoded, err := json.Marshal(alert)
	if err != nil {
		log.Error(err, "Unable to encode a Koney alert")
		return
	}

	// alerts are written as plain JSON lines, so that they can be picked up from the log as they are
	fmt.Println(string(encoded))

	for _, sink := range sinks {
		if err := f.sendAlert(ctx, alert, sink); err != nil {
			log.Error(err, "Unable to send an alert to a sink", "sink", sink.name)
		}
	}
}
