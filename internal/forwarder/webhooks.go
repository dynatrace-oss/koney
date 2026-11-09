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
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	k8slog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	// defaultAddress is the address where the alert webhooks are served.
	defaultAddress = ":8000"

	// maxRequestSize is the largest alert body that is read from a caller (1 MiB).
	maxRequestSize = 1 << 20

	// webhookShutdownTimeout bounds how long in-flight alerts are waited for on shutdown.
	webhookShutdownTimeout = 5 * time.Second
)

type webhookServer struct {
	*Forwarder
}

func (*webhookServer) NeedLeaderElection() bool {
	return false
}

// Start serves the webhooks until the context is cancelled.
func (f *webhookServer) Start(ctx context.Context) error {
	log := k8slog.FromContext(ctx).WithName("forwarder")
	requestCtx := k8slog.IntoContext(context.Background(), log)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /handlers/tetragon", f.handleTetragon())
	mux.HandleFunc("POST /handlers/kive", f.handleKive())

	listener, err := net.Listen("tcp", f.Address)
	if err != nil {
		return err
	}

	shutdownTimeout := webhookShutdownTimeout
	server := &manager.Server{
		Name: "alert-webhooks",
		Server: &http.Server{
			Handler:           mux,
			BaseContext:       func(net.Listener) context.Context { return requestCtx },
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
		Listener:        listener,
		ShutdownTimeout: &shutdownTimeout,
	}

	f.ready.Store(true)
	defer f.ready.Store(false)

	return server.Start(ctx)
}

// handleTetragon takes the trigger of Tetragon, which says that there are new events to pull.
func (f *Forwarder) handleTetragon() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if !f.authenticate(request) {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}

		select {
		case f.triggers <- struct{}{}:
		default: // a pull is already pending, no need to queue another one
		}

		writer.WriteHeader(http.StatusAccepted)
	}
}

// handleKive takes a single alert of Kive and forwards it right away. Kive reaches
// exactly one replica through the webhook service, so the alert is forwarded by whichever
// replica receives it, no matter whether that replica is the leader.
func (f *Forwarder) handleKive() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if !f.authenticate(request) {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxRequestSize))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(writer, "alert too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(writer, "unable to read the alert", http.StatusBadRequest)
			return
		}

		alert, err := mapKiveAlert(body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}

		writer.Header().Set("Content-Length", "0")
		writer.WriteHeader(http.StatusAccepted)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}

		ctx := context.WithoutCancel(request.Context())
		f.forwardAlert(ctx, alert, f.readAlertSinks(ctx))
	}
}

// authenticate checks that the caller knows the token that Koney puts into the webhook URLs.
func (f *Forwarder) authenticate(request *http.Request) bool {
	token := request.URL.Query().Get("token")
	return subtle.ConstantTimeCompare([]byte(token), []byte(f.Token)) == 1
}
