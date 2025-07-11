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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os" // Import sync for mutex
	"time"

	"github.com/gorilla/mux"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	K8sAuthError = "Failed to authenticate with Kubernetes API"
	// The delay after receiving a (possibly multiple) triggers until we start loading alerts (once)
	DEBOUNCE_SECONDS = 5 * time.Second // Define debounce duration
)

var (
	kubeClient  *kubernetes.Clientset
	triggerChan chan struct{} // Channel to signal new triggers for debouncer
)

func main() {
	// Set up logging to stderr so it appears in kubectl logs
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Initialize Kubernetes client
	if err := initKubernetesClient(); err != nil {
		log.Fatalf("Error initializing Kubernetes client: %v", err)
	}
	fmt.Println("DEBUG: Kubernetes client initialized successfully")

	// Initialize the trigger channel for debouncing
	triggerChan = make(chan struct{}, 1)

	// Start the debouncer goroutine in the background
	go startDebouncer()
	fmt.Println("DEBUG: Debouncer goroutine started.")

	router := mux.NewRouter()

	// Health check endpoint
	router.HandleFunc("/healthz", healthz).Methods("GET")
	fmt.Println("DEBUG: Registered /healthz endpoint")

	// Tetragon handler endpoint - this will now trigger event processing
	router.HandleFunc("/handlers/tetragon", handleTetragon).Methods("GET")
	fmt.Println("DEBUG: Registered /handlers/tetragon endpoint for event triggering.")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000" // Default port, consistent with Python version
	}
	addr := ":" + port

	fmt.Printf("DEBUG: Server starting on port %s...\n", port)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// Loads in-cluster config and creates a Kubernetes client.
func initKubernetesClient() error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to load in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}
	kubeClient = clientset
	return nil
}

// authenticateKubernetes checks if the Kubernetes client is initialized and can connect.
func authenticateKubernetes() bool {
	if kubeClient == nil {
		return false
	}
	// Perform a lightweight API call to verify connectivity
	_, err := kubeClient.DiscoveryClient.ServerVersion()
	return err == nil
}

// startDebouncer listens for signals on triggerChan and executes processTetragonEvents
// after a specified debounce period without new signals.
func startDebouncer() {
	var timer *time.Timer
	for range triggerChan {
		// If a timer is already running, stop it to reset the debounce period
		if timer != nil {
			timer.Stop()
		}
		// Start a new timer. When it expires, process events.
		timer = time.AfterFunc(DEBOUNCE_SECONDS, func() {
			fmt.Println("DEBUG: Debounce period ended. Processing Tetragon events...")
			processTetragonEvents()
		})
	}
}

// handleTetragon receives HTTP requests and signals the debouncer to process events.
func handleTetragon(w http.ResponseWriter, r *http.Request) {
	fmt.Println("DEBUG: Received trigger for /handlers/tetragon endpoint.")

	if !authenticateKubernetes() {
		http.Error(w, K8sAuthError, http.StatusUnauthorized)
		return
	}

	// Ensure the HTTP handler returns quickly.
	select {
	case triggerChan <- struct{}{}:
		fmt.Println("DEBUG: Signal sent to debouncer.")
	default:
		fmt.Println("DEBUG: Debouncer channel already has a pending signal or is full, skipping this trigger.")
	}

	// Respond with HTTP 202 Accepted
	w.WriteHeader(http.StatusAccepted)
}

// Reads and processes Tetragon events.
func processTetragonEvents() {
	// Read events from the last 60 seconds
	eventsPerPolicy, err := ReadTetragonEvents(kubeClient, 60) // Assuming ReadTetragonEvents takes kubeClient
	if err != nil {
		log.Printf("Error reading Tetragon events: %v", err)
		return
	}

	if len(eventsPerPolicy) == 0 {
		fmt.Println("DEBUG: No events found in this processing cycle.")
		return
	}

	for policyName, events := range eventsPerPolicy {
		fmt.Printf("DEBUG: Processing %d events for policy %s\n", len(events), policyName)

		for _, event := range events {
			koneyEvent := MapTetragonEvent(kubeClient, event) // Assuming MapTetragonEvent takes kubeClient
			if IsFilteredEvent(koneyEvent, KoneyFingerprint) {
				fmt.Println("DEBUG: Filtered event with fingerprint")
				continue
			}

			koneyEventJSON, err := json.Marshal(koneyEvent)
			if err != nil {
				log.Printf("Error marshaling event: %v", err)
				continue
			}

			// Output to stdout like Python version
			fmt.Println(string(koneyEventJSON))
		}
	}
}

// Provides a health check endpoint.
func healthz(w http.ResponseWriter, r *http.Request) {
	if !authenticateKubernetes() {
		http.Error(w, K8sAuthError, http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent) // HTTP 204 No Content
}
