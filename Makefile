GO ?= $(shell which go)
OS ?= $(shell $(GO) env GOOS)
ARCH ?= $(shell $(GO) env GOARCH)

# Repository and tag of the webhook container image. Override on the CLI:
#   make build IMAGE=registry.example.com/team/cert-manager-webhook-nameshield TAG=v0.1.0
IMAGE ?= cert-manager-webhook-nameshield
TAG ?= latest

ENVTEST_K8S_VERSION ?= 1.35.0

# Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin

.PHONY: all
all: build-image

.PHONY: test
## test runs the DNS01 conformance suite. It requires:
##   - NAMESHIELD_ZONE: a real Nameshield zone, e.g. example.com
##   - NAMESHIELD_TOKEN: a Nameshield API token with DNS rights on that zone
test: setup-envtest
	TEST_ASSET_ETCD=$(LOCALBIN)/k8s/$(ENVTEST_K8S_VERSION)-$(OS)-$(ARCH)/etcd \
	TEST_ASSET_KUBE_APISERVER=$(LOCALBIN)/k8s/$(ENVTEST_K8S_VERSION)-$(OS)-$(ARCH)/kube-apiserver \
	TEST_ASSET_KUBECTL=$(LOCALBIN)/k8s/$(ENVTEST_K8S_VERSION)-$(OS)-$(ARCH)/kubectl \
	$(GO) test -v .

.PHONY: unit-test
## unit-test runs only the unit tests, no envtest binaries needed.
unit-test:
	$(GO) test -v ./...

.PHONY: build
## build compiles the webhook binary (CGO disabled).
build:
	CGO_ENABLED=0 $(GO) build -o _out/webhook .

.PHONY: build-image
## build-image builds the container image with Docker.
build-image:
	docker build -t "$(IMAGE):$(TAG)" .

.PHONY: push-image
## push-image pushes the container image.
push-image:
	docker push "$(IMAGE):$(TAG)"

.PHONY: rendered-manifest
## rendered-manifest renders the Helm chart with the current IMAGE and TAG.
rendered-manifest:
	helm template cert-manager-webhook-nameshield \
	    --set image.repository=$(IMAGE) \
	    --set image.tag=$(TAG) \
	    deploy/cert-manager-webhook-nameshield

$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

ENVTEST ?= $(LOCALBIN)/setup-envtest

.PHONY: setup-envtest
setup-envtest: envtest
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path || { \
		echo "Error: failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST)
$(ENVTEST): $(LOCALBIN)
	GOBIN="$(LOCALBIN)" $(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
