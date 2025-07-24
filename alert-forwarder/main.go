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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// Error message for failed Kubernetes authentication
	K8S_AUTH_ERROR = "Failed to authenticate with Kubernetes API"
	// Delay after receiving triggers until we start loading alerts
	DEBOUNCE_SECONDS = 5 * time.Second
)

// Handles HTTP requests and Kubernetes operations
type alertHandler struct {
	kubeClient    *kubernetes.Clientset
	dynamicClient dynamic.Interface
	triggerChan   chan struct{}
}

func main() {
	Info("Alert-forwarder starting...")

	// Initialize Kubernetes client
	var kubeClient *kubernetes.Clientset
	var dynamicClient dynamic.Interface
	var err error

	for retries := 0; retries < 3; retries++ {
		kubeClient, dynamicClient, err = initKubernetesClient()
		if err != nil {
			Error("Kubernetes client error (attempt %d/3): %v", retries+1, err)
			if retries < 2 {
				Info("Retrying in 10 seconds...")
				time.Sleep(10 * time.Second)
				continue
			}
			Error("Failed to initialize Kubernetes client after 3 attempts: %v", err)
			os.Exit(1)
		}
		break
	}

	// Handler with dependencies
	handler := &alertHandler{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
		triggerChan:   make(chan struct{}, 1),
	}

	// Start the debouncer goroutine
	go handler.startDebouncer()
	Debug("Debouncer goroutine started")

	router := mux.NewRouter()

	// Register handlers
	router.HandleFunc("/healthz", handler.healthz).Methods("GET")
	router.HandleFunc("/handlers/tetragon", handler.handleTetragon).Methods("GET")
	Debug("Route handlers registered")

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	Info("Starting server on port %s", port)
	if err := http.ListenAndServe(":"+port, router); err != nil {
		Error("Server failed to start: %v", err)
		os.Exit(1)
	}
}

// Loads in-cluster config and creates clients
func initKubernetesClient() (*kubernetes.Clientset, dynamic.Interface, error) {
	Debug("Initializing Kubernetes client")
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	Debug("Kubernetes client initialized successfully")
	return clientset, dynamicClient, nil
}

// Listens for signals on triggerChan and executes processTetragonEvents
func (h *alertHandler) startDebouncer() {
	Debug("Debouncer started and waiting for triggers")
	var timer *time.Timer

	defer func() {
		if r := recover(); r != nil {
			Error("Debouncer panic recovered: %v", r)
		}
	}()

	for range h.triggerChan {
		Debug("Signal received in debouncer")

		if timer != nil {
			timer.Stop()
			Debug("Previous timer stopped")
		}

		timer = time.AfterFunc(DEBOUNCE_SECONDS, func() {
			Debug("Debounce period ended. Processing Tetragon events...")
			if err := h.processRecentAlerts(); err != nil {
				Error("Failed to process recent alerts: %v", err)
			}
		})
		Debug("New timer set for %v", DEBOUNCE_SECONDS)
	}
}

// Receives HTTP requests and signals the debouncer to process events
func (h *alertHandler) handleTetragon(w http.ResponseWriter, r *http.Request) {
	Debug("Received trigger for /handlers/tetragon endpoint")

	if !h.authenticateKubernetes() {
		Error("Kubernetes authentication failed")
		http.Error(w, K8S_AUTH_ERROR, http.StatusUnauthorized)
		return
	}

	select {
	case h.triggerChan <- struct{}{}:
		Debug("Signal sent to debouncer")
	default:
		Debug("Signal channel full, already has pending signal")
	}

	w.WriteHeader(http.StatusAccepted)
}

// Reads and processes Tetragon events
func (h *alertHandler) processRecentAlerts() error {
	Debug("Starting to process recent alerts...")

	// Read events from the last 60 seconds
	eventsPerPolicy, err := ReadTetragonEvents(h.kubeClient, 60)
	if err != nil {
		return fmt.Errorf("failed to read Tetragon events: %w", err)
	}

	if len(eventsPerPolicy) == 0 {
		Debug("No events found in any policy")
		return nil
	}

	totalProcessed := 0
	totalFiltered := 0

	for policyName, events := range eventsPerPolicy {
		Debug("Processing %d events for policy %s", len(events), policyName)

		for i, event := range events {
			koneyEvent := MapTetragonEvent(h.kubeClient, h.dynamicClient, event)

			if IsFilteredEvent(koneyEvent, KoneyFingerprint) {
				totalFiltered++
				Debug("Event %d filtered (fingerprint match)", i+1)
				continue
			}

			koneyEventJSON, err := json.Marshal(koneyEvent)
			if err != nil {
				Error("Failed to marshal event %d: %v", i+1, err)
				continue
			}

			fmt.Println(string(koneyEventJSON))
			totalProcessed++
			Debug("Alert %d generated successfully", totalProcessed)
		}
	}

	Debug("Processing complete. Generated %d alerts, filtered %d events", totalProcessed, totalFiltered)
	return nil
}

// Health check endpoint
func (h *alertHandler) healthz(w http.ResponseWriter, r *http.Request) {
	Debug("Health check requested")

	if !h.authenticateKubernetes() {
		Error("Health check failed - Kubernetes authentication error")
		http.Error(w, K8S_AUTH_ERROR, http.StatusServiceUnavailable)
		return
	}

	Debug("Health check passed")
	w.WriteHeader(http.StatusNoContent)
}

// Checks if the Kubernetes client is initialized
func (h *alertHandler) authenticateKubernetes() bool {
	if h.kubeClient == nil {
		Error("Kubernetes client is nil")
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := h.kubeClient.CoreV1().Pods(TetragonNamespace).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		Error("Kubernetes authentication failed: %v", err)

		// Fallback: try to list pods in our own namespace
		currentNamespace := getCurrentNamespace()
		if currentNamespace != "" {
			Debug("Trying fallback authentication with namespace: %s", currentNamespace)
			_, fallbackErr := h.kubeClient.CoreV1().Pods(currentNamespace).List(ctx, metav1.ListOptions{Limit: 1})
			if fallbackErr != nil {
				Error("Fallback authentication also failed: %v", fallbackErr)
				return false
			}
			Debug("Fallback authentication succeeded")
			return true
		}
		return false
	}

	Debug("Kubernetes authentication succeeded")
	return true
}

// Reads current namespace from the service account token
func getCurrentNamespace() string {
	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		Debug("Could not read current namespace: %v", err)
		return "koney-system"
	}
	return string(namespace)
}
