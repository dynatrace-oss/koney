#!make

# Include environment variables from .env file if it exists
ifneq (,$(wildcard .env))
    include .env
endif

# VERSION defines the version of Koney.
# To generate artifacts for another specific version temporarily, you can:
# - set the VERSION argument (e.g make docker-build VERSION=0.0.2)
# - set the VERSION environment variable (e.g export VERSION=0.0.2)
# - set the VERSION environment variable in the .env file (e.g VERSION=0.0.2 in .env)
VERSION ?= 0.1.0

# IMAGE_TAG_BASE defines the "basename" of the full name of container images.
# Currently, two images are built: the 'controller' and the 'alert-forwarder', i.e., resulting in
# $(IMAGE_TAG_BASE)-controller:$(VERSION) and $(IMAGE_TAG_BASE)-alert-forwarder:$(VERSION).
IMAGE_TAG_BASE ?= localhost:5001/koney

IMG_CONTROLLER_NAME ?= $(IMAGE_TAG_BASE)-controller
IMG_ALERT_FORWARDER_NAME ?=$(IMAGE_TAG_BASE)-alert-forwarder

IMG_CONTROLLER ?= $(IMG_CONTROLLER_NAME):$(VERSION)
IMG_ALERT_FORWARDER ?=$(IMG_ALERT_FORWARDER_NAME):$(VERSION)

# OPERATOR_SDK_VERSION sets the Operator SDK version to use.
# By default, what is installed on the system is used.
OPERATOR_SDK_VERSION ?= v1.41.0

# KIND_CLUSTER defines the name of the Kind cluster to be used for e2e tests.
KIND_CLUSTER ?= koney-test-e2e

# DEFAULT_NAMESPACE defines the default namespace to use for local deployments.
DEFAULT_NAMESPACE ?= koney-system

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
CONTAINER_TOOL ?= docker

# SHELL and flags to use for all recipes
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate CustomResourceDefinition YAML manifests from API definitions.
	@echo "Generating CustomResourceDefinition YAML manifests from API definitions ..."
	$(CONTROLLER_GEN) crd paths="./..." output:crd:artifacts:config=dist/chart/templates/crd
	@echo "Patching CustomResourceDefinition YAML manifests ..."
	./hack/make/patch-crds.sh dist/chart/templates/crd

.PHONY: generate
generate: controller-gen goimports ## Generate Go code for deep-copy from API definitions.
	@echo "Generating zz_generated.deepcopy.go from API definitions ..."
	$(CONTROLLER_GEN) object paths="./..."
	$(GOIMPORTS) -l -w -local github.com/dynatrace-oss/koney .

##@ Quality

.PHONY: fmt
fmt: goimports ## Run go fmt against code.
	gofmt -l -s -w .
	$(GOIMPORTS) -l -w -local github.com/dynatrace-oss/koney .

.PHONY: lint
lint: golangci-lint ## Run vet and golangci-lint linter.
	go vet ./...
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes.
	$(GOLANGCI_LINT) run --fix

.PHONY: test
test: generate fmt lint setup-envtest ## Run unit tests (no cluster required).
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)"  go test $(shell go list ./... | grep -v /test/) -coverprofile cover.out

.PHONY: test-e2e
test-e2e: generate fmt lint ## Run end-to-end tests (requires an isolated environment).
	KIND_CLUSTER=$(KIND_CLUSTER) go test ./test/e2e/ -v -ginkgo.v
	$(MAKE) clean-test-e2e

.PHONY: setup-test-e2e
setup-test-e2e: ## Create a Kind cluster and local registry for end-to-end tests.
	@command -v $(KIND) >/dev/null 2>&1 || { \
		echo "Kind is not installed. Please install Kind manually."; \
		exit 1; \
	}
	@case "$$($(KIND) get clusters)" in \
		*"$(KIND_CLUSTER)"*) \
			echo "Kind cluster '$(KIND_CLUSTER)' already exists. Skipping creation." ;; \
		*) \
			echo "Creating Kind cluster '$(KIND_CLUSTER)'..."; \
			KIND_CLUSTER_NAME=$(KIND_CLUSTER) CONTAINER_TOOL=$(CONTAINER_TOOL) KIND=$(KIND) sh ./hack/make/kind-with-registry.sh ;; \
	esac

.PHONY: clean-test-e2e
clean-test-e2e: ## Tear down the Kind cluster and local registry used for end-to-end tests.
	@$(KIND) delete cluster --name $(KIND_CLUSTER)
	$(CONTAINER_TOOL) rm -f kind-registry

##@ Build

.PHONY: build
build: generate fmt lint ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: generate fmt lint ## Run a controller from your host.
	go run ./cmd/main.go

.PHONY: docker-build
docker-build: ## Build docker image with the manager and alert forwarder.
	$(CONTAINER_TOOL) build --tag ${IMG_CONTROLLER} .
	$(CONTAINER_TOOL) build --tag ${IMG_ALERT_FORWARDER} ./alert-forwarder

PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager and alert forwarder for cross-platform support.
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG_CONTROLLER} .
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG_ALERT_FORWARDER} ./alert-forwarder
	- $(CONTAINER_TOOL) buildx rm project-v3-builder

.PHONY: docker-push
docker-push: ## Push docker image with the manager and alert forwarder.
	$(CONTAINER_TOOL) push ${IMG_CONTROLLER}
	$(CONTAINER_TOOL) push ${IMG_ALERT_FORWARDER}

# TODO (#18): Once we integrate the alert-forwarder into the controller image (#18), simplify the sed substitutions here.
.PHONY: helm-package
helm-package: manifests helm ## Generate a Helm chart from the installer YAML.
	echo "Patching Chart.yaml ..."
	sed -i 's/^version: .*/version: ${VERSION}/' dist/chart/Chart.yaml
	sed -i 's/^appVersion: .*/appVersion: "${VERSION}"/' dist/chart/Chart.yaml
	echo "Patching values.yaml ..."
	sed -i 's|^\([[:space:]]*\)repository: \(\S*\) # patch:manager|\1repository: ${IMG_CONTROLLER_NAME} # patch:manager|' dist/chart/values.yaml
	sed -i 's|^\([[:space:]]*\)repository: \(\S*\) # patch:alert-forwarder|\1repository: ${IMG_ALERT_FORWARDER_NAME} # patch:alert-forwarder|' dist/chart/values.yaml
	sed -i 's|^\([[:space:]]*\)tag: .*|\1tag: ${VERSION}|' dist/chart/values.yaml
	$(HELM) package --app-version ${VERSION} -u -d dist ./dist/chart
	$(HELM) lint ./dist/chart

.PHONY: helm-render
helm-render: helm-package helm ## Generate a consolidated YAML rendered from the Helm chart.
	$(HELM) template --namespace $(DEFAULT_NAMESPACE) \
		--set manager.image.repository=${IMG_CONTROLLER_NAME} \
		--set manager.image.tag=${VERSION} \
		--set alertForwarder.image.repository=${IMG_ALERT_FORWARDER_NAME} \
		--set alertForwarder.image.tag=${VERSION} \
		--set template.helmLabels=false \
		--set template.createNamespace=true \
		koney ./dist/chart > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = true
endif

.PHONY: deploy
deploy: manifests helm ## Deploy Koney to the currently configured cluster.
	$(HELM) template --namespace $(DEFAULT_NAMESPACE) \
		--set manager.image.repository=${IMG_CONTROLLER_NAME} \
		--set manager.image.tag=${VERSION} \
		--set alertForwarder.image.repository=${IMG_ALERT_FORWARDER_NAME} \
		--set alertForwarder.image.tag=${VERSION} \
		--set template.helmLabels=false \
		--set template.createNamespace=true \
		koney ./dist/chart | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: helm ## Undeploy Koney from the currently configured cluster. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(HELM) template --namespace $(DEFAULT_NAMESPACE) --set template.createNamespace=true koney ./dist/chart | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -
##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
GOIMPORTS = $(LOCALBIN)/goimports
KUBEBUILDER ?= $(LOCALBIN)/kubebuilder
HELM ?= $(LOCALBIN)/helm

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.18.0
# ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
# ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
GOLANGCI_LINT_VERSION ?= v2.1.0
GOIMPORTS_VERSION ?= v0.39.0
KUBEBUILDER_VERSION ?= v4.10.1
HELM_VERSION ?= v3.19.2

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: goimports
goimports: $(GOIMPORTS) ## Download goimports locally if necessary.
$(GOIMPORTS): $(LOCALBIN)
	$(call go-install-tool,$(GOIMPORTS),golang.org/x/tools/cmd/goimports,$(GOIMPORTS_VERSION))

.PHONY: kubebuilder
kubebuilder: $(KUBEBUILDER) ## Download kubebuilder locally if necessary.
$(KUBEBUILDER): $(LOCALBIN)
	$(call go-install-tool,$(KUBEBUILDER),sigs.k8s.io/kubebuilder/v4,$(KUBEBUILDER_VERSION))

.PHONY: helm
helm: $(HELM) ## Download helm locally if necessary.
$(HELM): $(LOCALBIN)
	$(call go-install-tool,$(HELM),helm.sh/helm/v3/cmd/helm,$(HELM_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (, $(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif
endif
