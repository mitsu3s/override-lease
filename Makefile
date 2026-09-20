IMG ?= ghcr.io/mitsu3s/override-lease:latest
CONTAINER_TOOL ?= docker
PLATFORM ?= linux/amd64

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

LOCALBIN ?= $(shell pwd)/bin
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
KUSTOMIZE ?= $(LOCALBIN)/kustomize
STATICCHECK ?= $(LOCALBIN)/staticcheck
KUBECTL ?= kubectl

CONTROLLER_TOOLS_VERSION ?= v0.21.0
KUSTOMIZE_VERSION ?= v5.8.1
STATICCHECK_VERSION ?= v0.8.1

.PHONY: all
all: build

.PHONY: manifests
manifests: controller-gen
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen
	$(CONTROLLER_GEN) object paths="./..."

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: test
test: manifests generate fmt vet
	go test ./...

.PHONY: test-race
test-race: manifests generate fmt vet
	go test -race ./...

.PHONY: lint
lint: staticcheck
	$(STATICCHECK) ./...

.PHONY: build
build: manifests generate fmt vet
	go build -o bin/manager ./cmd

.PHONY: run
run: manifests generate fmt vet
	go run ./cmd

.PHONY: install
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=true -f -

.PHONY: deploy
deploy: manifests kustomize
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: kustomize
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=true -f -

.PHONY: docker-build
docker-build:
	$(CONTAINER_TOOL) build --platform $(PLATFORM) -t $(IMG) .

.PHONY: docker-push
docker-push:
	$(CONTAINER_TOOL) push $(IMG)

.PHONY: docker-publish
docker-publish:
	$(CONTAINER_TOOL) buildx build --platform $(PLATFORM) -t $(IMG) --push .

.PHONY: docker-inspect
docker-inspect:
	$(CONTAINER_TOOL) buildx imagetools inspect $(IMG)

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN)

$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(CONTROLLER_GEN) || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: kustomize
kustomize: $(KUSTOMIZE)

$(KUSTOMIZE): $(LOCALBIN)
	test -s $(KUSTOMIZE) || GOBIN=$(LOCALBIN) go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: staticcheck
staticcheck: $(STATICCHECK)

$(STATICCHECK): $(LOCALBIN)
	test -s $(STATICCHECK) || GOBIN=$(LOCALBIN) go install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
