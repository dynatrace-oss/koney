# Local debugging of Koney

This document describes how to debug Koney locally with VS Code.

## Configure the `launch.json` file

Create a `.vscode/launch.json` file unless it exists already.
This file will launch `main.go` with the `go` debugger.

```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Launch Koney",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "${workspaceFolder}/cmd/main.go"
        }
    ]
}
```

Then, just start debugging and you are good to go.

## Starting the debugger with the `koney-manager-serviceaccount` service account

Local debugging uses your privileged user account to access the Kubernetes cluster.
However, the operator runs with the `koney-manager-serviceaccount` service account later.
To fully imitate the operator's behavior locally, you can also start the debugger with the `koney-manager-serviceaccount` service account.
This is not required, but it can be useful when debugging permission-related issues locally.

First, we must [create a non-expiring, persisted API token](https://kubernetes.io/docs/reference/access-authn-authz/service-accounts-admin/#create-token)
for the `koney-manager-serviceaccount` service account. If not already created, use the following command:
```sh
kubectl apply -f ./hack/koney-manager-debugger-secret.yaml
```

Then, describe the secret to get the token:

```sh
kubectl -n koney-system describe secret koney-manager-debugger-secret
```

Then, we add a new user to your `~/.kube/config` file that uses this token with the following command:

```sh
kubectl config set-credentials koney-manager-serviceaccount --token=PUT_THE_TOKEN_HERE
```

Finally, we need to create a new context that uses the `koney-manager-serviceaccount` user.
It is recommended to copy an existing context and just replace the `user` field with `koney-manager-serviceaccount`.
You can view your contexts with the following command. Your current context is marked with `*`.

```sh
kubectl config get-contexts
```

Then, create a new context (with a new name) with the following command.
Make sure to replace `CLUSTER_NAME` with the name of the cluster in your current context.
We also recommend using the same name as the current context with the suffix `-as-koney`.

```sh
kubectl config set-context CONTEXT_NAME-as-koney --cluster=CLUSTER_NAME --user=koney-manager-serviceaccount
```

Activate the new context with the following command:

```sh
kubectl config use-context CONTEXT_NAME-as-koney
```

Now start the debugger. It will use this context to access the Kubernetes cluster.
