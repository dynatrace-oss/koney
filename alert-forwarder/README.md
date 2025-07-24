# Alert Forwarder for Koney

The alert-forwarder is a single Go package application consisting of:

- `main.go` - HTTP server with endpoints for Tetragon webhook and health checks
- `logger.go` - Centralized logging system with configurable log levels
- `fingerprint.go` - Encodes fingerprints for filtering self-generated events
- `tetragon.go` - Reads and processes Tetragon events from logs
- `types.go` - Common type definitions for alerts

## Features

- **Lightweight**: Minimal dependencies, efficient Go implementation
- **Debounced processing**: Prevents excessive API calls when receiving multiple triggers
- **Event deduplication**: Filters duplicate events using hash-based caching
- **Fingerprint filtering**: Ignores self-generated events using encoded fingerprints
- **Kubernetes native**: Uses in-cluster configuration and client-go
- **Configurable logging**: Environment-driven log levels for development and production

## API Endpoints

- `GET /handlers/tetragon` - Webhook endpoint triggered by Tetragon
- `GET /healthz` - Health check endpoint

## Configuration

The service uses the following environment variables:

### Required Configuration

- `PORT` - HTTP server port (default: 8000)

### Logging Configuration

- `DEBUG` - Enable debug logging (default: false)
  - `DEBUG=true` - Enables verbose debug logging
  - `DEBUG=false` - Production logging (info, warnings, errors only)
- `LOG_LEVEL` - Granular log level control (overrides DEBUG)
  - `LOG_LEVEL=debug` - All logs (most verbose)
  - `LOG_LEVEL=info` - Info, warnings, and errors (default)
  - `LOG_LEVEL=warn` - Warnings and errors only
  - `LOG_LEVEL=error` - Errors only

### Examples

```bash
# Production mode (clean logs)
docker run koney-alert-forwarder

# Development mode (verbose logs)
docker run -e DEBUG=true koney-alert-forwarder

# Custom log level
docker run -e LOG_LEVEL=warn koney-alert-forwarder
```

## Logging

The alert-forwarder uses a centralized logging system with consistent formatting across all components:

### Log Levels

- **DEBUG**: Detailed processing information, event parsing, metadata extraction
- **INFO**: Important operational events (startup, server status)
- **WARN**: Non-fatal issues (pod not ready, parsing failures)
- **ERROR**: Critical errors that affect functionality

### Production vs Development

- **Production**: Clean output with only essential information and JSON alerts
- **Development**: Verbose debugging with step-by-step processing details

### Log Output Example

**Production Mode (LOG_LEVEL=info)**:

```txt
INFO: Alert-forwarder starting...
INFO: Starting server on port 8000
{"timestamp":"2025-07-23T01:08:09Z","deception_policy_name":"deceptionpolicy-servicetoken",...}
```

**Development Mode (DEBUG=true)**:

```txt
INFO: Alert-forwarder starting...
DEBUG: Kubernetes client initialized successfully
DEBUG: Debouncer goroutine started
DEBUG: Route handlers registered
DEBUG: Looking for Tetragon pods with label selector: app.kubernetes.io/name=tetragon
DEBUG: Found 1 Tetragon pods
DEBUG: Processing pod 1/1: tetragon-xqxcp (status: Running)
DEBUG: Found potential Tetragon event line (match 1): {"process_kprobe":...
DEBUG: Mapping Tetragon event to KoneyAlert
DEBUG: Set trap type to filesystem_honeytoken with metadata: map[file_path:/run/secrets/koney/service_token]
{"timestamp":"2025-07-23T01:08:09Z","deception_policy_name":"deceptionpolicy-servicetoken",...}
DEBUG: Alert 1 generated successfully
```

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
