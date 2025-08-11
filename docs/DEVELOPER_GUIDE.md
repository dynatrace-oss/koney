# 💻 Developer Guide

This document describes how to build, test, and deploy the Koney operator.

## 📋 Prerequisites

- `go` version v1.24.0+
- `docker` version 17.03+
- `kubectl` version v1.11.3+
- Access to a Kubernetes v1.11.3+ cluster.
- [Tetragon](https://tetragon.io/) v1.1.0+ or [Kive](https://github.com/San7o/kivebpf) installed in the cluster, if you also want to monitor traps.
- `pre-commit` to run checks before committing changes.

Other dependencies such as `operator-sdk` and `helm` are automatically downloaded to `bin/` when needed.

## ⚓ Deploy the Operator to the Cluster

Build and push the images with the following command.
The images are pushed to the registry specified in the `IMAGE_TAG_BASE` variable with the `VERSION` tag.
Currently, we build and push two images, `your.local.registry/koney-controller:demo` and `your.local.registry/research/koney-alert-forwarder:demo`.

```sh
make docker-build docker-push IMAGE_TAG_BASE="your.local.registry/koney" VERSION="demo"
```

ℹ️ **Note:** You can create an `.env` file in the repository root to set arguments such as `IMAGE_TAG_BASE` easily.

Deploy the controller and all resources to the cluster with the following command.
If you encounter RBAC errors, you may need to grant yourself cluster-admin privileges or be logged in as admin.

```sh
make deploy IMAGE_TAG_BASE="your.local.registry/koney" VERSION="demo"
```

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

Undeploy the controller and all resources from the cluster:

```sh
make undeploy
```

## 🏗️ Project Distribution

Package the Helm chart for distribution:

```sh
make helm-package IMAGE_TAG_BASE="your.local.registry/koney" VERSION="x.y.z"
```

Render the Helm chart to create a consolidated YAML file for easy installation.
This creates a `install.yaml` file in the `dist` directory which can be applied directly to a cluster.

```sh
make helm-render IMAGE_TAG_BASE="your.local.registry/koney" VERSION="x.y.z"
```

Push the Helm chart to an OCI-compatible registry with the following command.
This pushes the chart package at `your.local.registry/koney/charts/koney:x.y.z`.
This can be overridden by setting the `HELM_REGISTRY` variable explicitly.

```sh
make helm-push IMAGE_TAG_BASE="your.local.registry/koney" VERSION="x.y.z"
```

### New Release Process

1. Bump the version in the `Makefile`
2. Bump the version in the `README.md` in the installation instructions
3. Build the images with `make docker-build` to verify that the build works
4. Package the chart with `make helm-package` to verify that the chart looks good
5. Render the chart with `make helm-render` to have the `install.yaml` ready
6. Commit, tag, e.g., with `v1.2.3`, push, and let the GitHub actions do the rest
7. Create a new release on GitHub

ℹ️ **Note**: Image version tags are formatted as `1.2.3` while git version tags are formatted as `v1.2.3` (with a `v` prefix).

## 🪲 Debugging

To see the logs of the Koney operator, use the following command:

```sh
kubectl logs -n koney-system -l control-plane=controller-manager
```

Please refer to the 📄 [DEBUGGING](./DEBUGGING.md) document for instructions on how to debug Koney locally with VS Code.

## 🔎 Testing

Run all unit tests.

```sh
make test
```

Run all end-to-end tests in a real cluster. Make sure to set the correct context to your playground cluster.

ℹ️ **Note**: Tetragon and Kive must be installed in the cluster to run all the end-to-end tests.

```sh
kubectl config set-context your-playground-cluster
make test-e2e
```

### Run tests manually with Ginkgo

Install Ginkgo locally first. Make sure to install a version compatible with the one used in the project.

```sh
go install github.com/onsi/ginkgo/v2/ginkgo@v2.23.4
```

Then, navigate to a directory with tests and run `ginkgo` there.

```sh
cd ./internal/controller
ginkgo -v
```

## 💖 Contributing

After cloning the repository, install the pre-commit hooks.

```sh
pre-commit install
```

This will then automatically run checks before committing changes.

You can run all checks manually with the following command:

```sh
pre-commit run --all-files
```
