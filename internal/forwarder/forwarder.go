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
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Options configure the alert forwarder.
type Options struct {
	// Client reads the resources that alerts are resolved against.
	Client client.Client

	// Clientset reads the logs of the Tetragon pods and the namespace whose uid identifies
	// the cluster, which the client cannot do without caching all namespaces.
	Clientset kubernetes.Interface

	// Namespace is the namespace where Koney is installed.
	Namespace string

	// Token authenticates the callers of the webhooks.
	Token string

	// Address is the address where the webhooks are served. Defaults to defaultAddress.
	Address string
}

// Forwarder receives the alerts of the captors and forwards them to the alert sinks.
// It runs on the controller manager, next to the controllers.
type Forwarder struct {
	Options

	triggers         chan struct{}
	events           *eventCache
	ready            atomic.Bool
	cachedClusterUID string
	clusterMutex     sync.Mutex
	lastReadAt       time.Time
	httpClient       *http.Client
}

// New builds the alert forwarder. Add both of its runnables to the manager.
func New(options Options) *Forwarder {
	if options.Address == "" {
		options.Address = defaultAddress
	}

	return &Forwarder{
		Options:    options,
		triggers:   make(chan struct{}, 1),
		events:     newEventCache(eventCacheSize),
		httpClient: &http.Client{Timeout: sinkRequestTimeout},
	}
}

// Webhooks returns the runnable that serves the alert webhooks. It runs on every replica,
// so that the callers of the webhooks are never refused while a replica waits for leadership.
// A trigger that arrives before the event puller runs is kept until the puller starts.
func (f *Forwarder) Webhooks() manager.Runnable {
	return &webhookServer{Forwarder: f}
}

// EventPuller returns the runnable that pulls the Tetragon events. It runs on the leading
// replica only, so that the sinks do not receive the same alert once per replica.
func (f *Forwarder) EventPuller() manager.Runnable {
	return &eventPuller{Forwarder: f}
}

// Ready reports whether the alert webhooks are being served.
func (f *Forwarder) Ready(_ *http.Request) error {
	if !f.ready.Load() {
		return errors.New("the alert webhooks are not served yet")
	}

	return nil
}
