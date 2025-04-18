
# 💻 Developer Guide

This document describes how to build, test, and deploy the Koney operator.

## 📋 Prerequisites

- `go` version v1.23.0+
- `docker` version 17.03+.
- `kubectl` version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.
- [Tetragon](https://tetragon.io/) v1.1.0+ installed in the cluster, if you also want to monitor traps.
- `pre-commit` to run checks before committing changes.

## ⚓ Deploy the Operator to the Cluster

Build and push the images with the following command.
The images are pushed to the registry specified in the `IMAGE_TAG_BASE` variable with the `VERSION` tag.

```sh
make docker-build docker-push IMAGE_TAG_BASE="your.local.registry/koney" VERSION="demo"
```

ℹ️ **Note:** This actually builds and pushes two images: `your.local.registry/koney-controller:demo` and `your.local.registry/research/koney-alert-forwarder:demo`.

ℹ️ **Note:** You can create an `.env` file in the repository root to set some arguments directly.

Before deploying the operator, install the Custom Resource Definitions (CRDs) to the cluster:

```sh
make install
```

Then, deploy the manager to the cluster with the specified version:

```sh
make deploy VERSION="demo"
```

ℹ️ **Note**: If you encounter RBAC errors, you may need to grant yourself cluster-admin privileges or be logged in as admin.

You can find samples (examples) deception policies in the `config/sample/` directory and apply them to the cluster.

```sh
kubectl apply -f config/samples/deceptionpolicy-servicetoken.yaml
```

ℹ️ **Note**: Ensure that the samples have proper default values for testing purposes.

## 🧹 Uninstall the Operator from the Cluster

Delete deception policies and give the operator a chance to clean up traps:

```sh
kubectl delete -f config/samples/deceptionpolicy-servicetoken.yaml
```

Undeploy the controller from the cluster:

```sh
make undeploy
```

Finally, delete the APIs (CRDs) from the cluster:

```sh
make uninstall
```

## 🏗️ Project Distribution

Build the installer for the image built and published in the registry:

```sh
make build-installer VERSION="x.y.z"
```

ℹ️ **Note**: The makefile target mentioned above generates an `install.yaml` file in the `dist` directory. This file contains all the resources built with Kustomize, which are necessary to install this project without its dependencies.

### New Release Process

1. Bump the version in the `Makefile`
2. Bump the version in the `README.md`
3. Build the installer (see above) with the new version
4. Commit, tag, push, and let the pipeline push the image to the registry
5. Create a new release on GitHub

ℹ️ **Note**: Image version tags are formatted as `1.2.3` while git version tags are formatted as `v1.2.3` (with a `v` prefix).

## 🪲 Debugging

To see the logs of the Koney operator, use the following command:

```sh
kubectl logs -n koney-system -l control-plane=controller-manager
```

Please refer to the 📄 [DEBUGGING](./DEBUGGING.md) document for instructions on how to debug Koney locally with VS Code.

## 🔎 Testing

Run all unit tests:

```sh
make test
```

If you are missing dependencies like `goimports`, install them first:

```sh
go install golang.org/x/tools/cmd/goimports@latest
```

Run all end-to-end tests:

⚠️ **Warning**: This will deploy resources in your currently active cluster! Make sure that this is a playground cluster.

ℹ️ **Note**: Tetragon must be installed in the cluster to run all the end-to-end tests.

```sh
kubectl config set-context your-playground-cluster
make test-e2e
```

Run test manually from the command line:

We use Ginkgo to run tests, make sure to have it installed locally.
The locally installed version must match the version that Koney has in its dependencies.

```sh
go install github.com/onsi/ginkgo/v2/ginkgo
```

Then, navigate to a directory with tests and run `ginkgo` there.
You will get a more context-rich output.

```sh
cd ./internal/controller
ginkgo -v
```

## 💖 Contributing

After cloning the repository, install the pre-commit hooks:

```sh
pre-commit install
```

This will then automatically run checks before committing changes.
