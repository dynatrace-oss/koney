# 🔔 Alert Sinks

When hackers access traps, Koney logs an event in the `alerts` container. You can also configure Koney to send these alerts to external systems. For this, you simply add an `DeceptionAlertSink` resource to your cluster. Forwarding is supported for all trap types.

ℹ️ **Note**: All `DeceptionAlertSink` resources and referenced `Secret` resources must be in the `koney-system` namespace. Resources in other namespaces will be ignored.


At the moment, we support sending alerts to the following systems:

- [Dynatrace Security Events](#dynatrace-security-events)

## Dynatrace Security Events

1. Open the **'Access Tokens'** app in your Dynatrace environment
2. Create a new access token with scope `openpipeline.events_security`. Follow the [official documentation](https://docs.dynatrace.com/docs/shortlink/api-authentication#token-format-prefixes) for more information.
3. Store the created token and your [environment ID](https://docs.dynatrace.com/docs/shortlink/monitoring-environment#environment-id) in a `Secret` resource in the same namespace as Koney (default: `koney-system`).

```yaml
kubectl create secret generic -n koney-system dynatrace-api-token \
  --from-literal=apiToken=dt0c01.EXAMPLE_TOKEN_REPLACE_THE_ENTIRE_STRING \
  --from-literal=apiUrl=https://ENVIRONMENTID.live.dynatrace.com
```

4. Create a `DeceptionAlertSink` resource in your cluster and point it to the `dynatrace-api-token` secret:

```yaml
apiVersion: research.dynatrace.com/v1alpha1
kind: DeceptionAlertSink
metadata:
  name: deceptionalertsink-dynatrace
  namespace: koney-system
spec:
  dynatrace:
    secretName: dynatrace-api-token
    severity: HIGH
```

The `dynatrace` section contains the following fields:

- `secretName`: The name of the `Secret` resource containing the `apiToken` and `apiUrl` fields.
- `severity`: The severity of the alert upon ingest. Possible values are `CRITICAL`, `HIGH`, `MEDIUM`, and `LOW`. The default value is `HIGH`.

To apply a deception alert sink resource, use the following command:

```sh
kubectl apply -f <deceptionalertsink-file>.yaml
```

ℹ️ **Note**: The `DeceptionAlertSink` resource and the referenced `Secret` resource must be in the same namespace as Koney (default: `koney-system`).

### Dynatrace Sink Format

Deception alerts sent to the `/platform/ingest/v1/security.events` endpoint will have the following format:

```yaml
{
  "timestamp": "2025-07-18T19:39:11Z",

  "koney.deception_policy_name": "deceptionpolicy-servicetoken",
  "koney.trap_type": "filesystem_honeytoken",
  "koney.metadata.file_path": "/run/secrets/koney/service_token",

  "event.kind": "SECURITY_EVENT",
  "event.type": "DETECTION_FINDING",
  "event.name": "Detection finding event",
  "event.provider": "Koney",
  "event.version": "2025-07-18",
  "event.id": "C85DB174F28C4FDD0892C4B8F280A86B",
  "event.description": "Access to honeytoken (/run/secrets/koney/service_token) in pod (koney-demo/koney-demo-deployment-8f9cb7b9c-q4fxl) detected",

  "finding.type": "KONEY_ALERT",
  "finding.id": "C85DB174F28C4FDD0892C4B8F280A86B",
  "finding.title": "Access to honeytoken (/run/secrets/koney/service_token) in pod (koney-demo/koney-demo-deployment-8f9cb7b9c-q4fxl) detected",
  "finding.description": "Access to honeytoken (/run/secrets/koney/service_token) in pod (koney-demo/koney-demo-deployment-8f9cb7b9c-q4fxl) detected",
  "finding.time.created": "2025-07-18T19:39:11Z",
  "finding.severity": "HIGH",

  "dt.security.risk.level": "HIGH",
  "dt.security.risk.score": 8.9,

  "product.name": "Koney",
  "product.vendor": "Dynatrace Research",

  "k8s.namespace.name": "koney-demo",
  "k8s.node.name": "minikube",
  "k8s.pod.name": "koney-demo-deployment-8f9cb7b9c-q4fxl",
  "k8s.container.name": "nginx",
  "k8s.container.id": "6f5ab819f146ffd24745bac5d3dc2c3d4071c504366fb85b416a7a500de144d9",

  "process.executable.name": "cat",
  "process.executable.path": "/usr/bin",
  "process.executable.arguments": "/run/secrets/koney/service_token",
  "process.pid": 9999,
  "process.uid": 0,
  "process.cwd": "/",

  "object.type": "KUBERNETES_CONTAINER_ID",
  "object.id": "6f5ab819f146ffd24745bac5d3dc2c3d4071c504366fb85b416a7a500de144d9",
}
```
