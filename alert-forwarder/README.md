# Alert Forwarder for Koney

This is the Go implementation of the alert-forwarder component for the Koney project. It monitors Tetragon events and forwards alerts when honeytokens or other traps are accessed.

## Architecture

The alert-forwarder is a single Go package application consisting of:

- `main.go` - HTTP server with endpoints for Tetragon webhook and health checks
- `fingerprint.go` - Encodes fingerprints for filtering self-generated events
- `tetragon.go` - Reads and processes Tetragon events from pod logs
- `types.go` - Common type definitions for alerts

All code is in the `main` package for simplicity and ease of maintenance.

## Features

- **Lightweight**: Minimal dependencies, efficient Go implementation
- **Debounced processing**: Prevents excessive API calls when receiving multiple triggers
- **Event deduplication**: Filters duplicate events using hash-based caching
- **Fingerprint filtering**: Ignores self-generated events using encoded fingerprints
- **Kubernetes native**: Uses in-cluster configuration and client-go

## API Endpoints

- `GET /handlers/tetragon` - Webhook endpoint triggered by Tetragon
- `GET /healthz` - Health check endpoint

## Configuration

The service uses the following environment variables:

- `PORT` - HTTP server port (default: 8080)

Kubernetes configuration is loaded automatically using in-cluster config.

## Alert Format

Alerts are output as JSON to stdout with the following structure:

```json
{
  "timestamp": "2025-01-03T18:47:56Z",
  "deception_policy_name": "deceptionpolicy-servicetoken",
  "trap_type": "filesystem_honeytoken",
  "metadata": {
    "file_path": "/run/secrets/koney/service_token"
  },
  "pod": {
    "name": "test-pod",
    "namespace": "test-namespace",
    "container": {
      "id": "docker://...",
      "name": "nginx"
    }
  },
  "process": {
    "pid": 148373,
    "cwd": "/",
    "binary": "/usr/bin/cat",
    "arguments": "/run/secrets/koney/service_token"
  }
}
```
