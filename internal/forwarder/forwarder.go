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
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8slog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	// defaultAddress is the address where the alert webhooks are served.
	defaultAddress = ":8000"

	// maxRequestSize is the largest alert body that is read from a caller.
	maxRequestSize = 1 << 20

	// maxLogLineSize is the largest Tetragon log line that is read.
	maxLogLineSize = 1 << 20

	// eventCacheSize is the number of recent Tetragon events that are remembered to skip duplicates.
	eventCacheSize = 10000

	// webhookShutdownTimeout bounds how long in-flight alerts are waited for on shutdown.
	webhookShutdownTimeout = 5 * time.Second
)

// clusterUIDNamespace is the namespace whose uid identifies the cluster.
const clusterUIDNamespace = "kube-system"

var (
	// debounceDelay is the quiet period that a trigger waits for, so that a burst
	// of triggers collapses into a single pull of the Tetragon events.
	debounceDelay = 5 * time.Second

	// maxDebounceDelay is the longest that a pull is held back, so that a steady
	// stream of triggers cannot postpone it forever.
	maxDebounceDelay = 30 * time.Second
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

type eventPuller struct {
	*Forwarder
}

func (*eventPuller) NeedLeaderElection() bool {
	return true
}

// Start pulls the Tetragon events until the context is cancelled.
func (f *eventPuller) Start(ctx context.Context) error {
	log := k8slog.FromContext(ctx).WithName("forwarder")
	f.pullLoop(k8slog.IntoContext(ctx, log))

	return nil
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

// pullLoop pulls the Tetragon events once a burst of triggers has calmed down.
func (f *Forwarder) pullLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.triggers:
			if !f.waitForQuietPeriod(ctx) {
				return
			}
			f.loadNewAlerts(ctx)
		}
	}
}

// waitForQuietPeriod waits until no trigger was received for debounceDelay,
// but never longer than maxDebounceDelay. It reports whether the wait finished.
func (f *Forwarder) waitForQuietPeriod(ctx context.Context) bool {
	quiet := time.NewTimer(debounceDelay)
	defer quiet.Stop()
	deadline := time.NewTimer(maxDebounceDelay)
	defer deadline.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-f.triggers:
			quiet.Reset(debounceDelay)
		case <-quiet.C:
			return true
		case <-deadline.C:
			return true
		}
	}
}

// loadNewAlerts pulls the Tetragon events of all Koney tracing policies and forwards them.
func (f *Forwarder) loadNewAlerts(ctx context.Context) {
	log := k8slog.FromContext(ctx)

	eventsPerPolicy := f.readTetragonEvents(ctx)
	if len(eventsPerPolicy) == 0 {
		return
	}

	sinks := f.readAlertSinks(ctx)

	for policyName, events := range eventsPerPolicy {
		log.V(1).Info("Transforming alerts", "policy", policyName, "events", len(events))

		// the selectors are only needed when Tetragon could not evaluate them itself
		containerSelectors := f.resolveContainerSelectors(ctx, policyName)

		for _, event := range events {
			alert := f.mapTetragonEvent(ctx, event)

			if isOwnAlert(alert) {
				log.V(1).Info("Skipping an alert that Koney triggered itself")
				continue
			}

			if containerSelectors != nil {
				containerName := ""
				if alert.Pod != nil {
					containerName = alert.Pod.Container.Name
				}
				if !containerMatchesSelectors(containerName, containerSelectors) {
					log.V(1).Info("Skipping an alert of a container that the trap does not select", "container", containerName)
					continue
				}
			}

			f.forwardAlert(ctx, alert, sinks)
		}
	}
}

func (f *Forwarder) readPodLog(ctx context.Context, podName string, sinceSeconds int64) (string, error) {
	options := &corev1.PodLogOptions{Container: tetragonPodContainerName, SinceSeconds: &sinceSeconds}

	stream, err := f.Clientset.CoreV1().Pods(tetragonNamespace).GetLogs(podName, options).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()

	logs, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}

	return string(logs), nil
}

// clusterUID returns the uid of the kube-system namespace, which identifies the cluster.
func (f *Forwarder) clusterUID(ctx context.Context) (string, error) {
	f.clusterMutex.Lock()
	defer f.clusterMutex.Unlock()

	if f.cachedClusterUID != "" {
		return f.cachedClusterUID, nil
	}

	namespace, err := f.Clientset.CoreV1().Namespaces().Get(ctx, clusterUIDNamespace, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	f.cachedClusterUID = string(namespace.UID)

	return f.cachedClusterUID, nil
}

func labelSelector(selector string) (client.MatchingLabelsSelector, error) {
	parsed, err := labels.Parse(selector)
	if err != nil {
		return client.MatchingLabelsSelector{}, err
	}

	return client.MatchingLabelsSelector{Selector: parsed}, nil
}

// schemePattern matches the prefix of a container id, such as "docker://".
var schemePattern = regexp.MustCompile(`^\w+://`)

func normalizeContainerID(containerID string) string {
	return strings.TrimSpace(schemePattern.ReplaceAllString(containerID, ""))
}

// eventCache remembers the Tetragon events that were already forwarded, by their hash.
type eventCache struct {
	mutex   sync.Mutex
	size    int
	seenSet map[uint64]struct{}
	order   []uint64
}

func newEventCache(size int) *eventCache {
	return &eventCache{size: size, seenSet: make(map[uint64]struct{}, size)}
}

// seen reports whether an event was already forwarded, and remembers it if it was not.
func (cache *eventCache) seen(event string) bool {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(event))
	key := hash.Sum64()

	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	if _, ok := cache.seenSet[key]; ok {
		return true
	}

	if len(cache.order) >= cache.size {
		delete(cache.seenSet, cache.order[0])
		cache.order = cache.order[1:]
	}

	cache.seenSet[key] = struct{}{}
	cache.order = append(cache.order, key)

	return false
}
