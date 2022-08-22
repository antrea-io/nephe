# Image URL to use all building/pushing image targets
IMG ?= antrea/nephe:latest
BUILDER_IMG ?= nephe/builder:latest

CRD_OPTIONS ?= "crd"

DOCKER_SRC=/usr/src/antrea.io/nephe
DOCKER_GOPATH=/tmp/gopath
DOCKER_GOCACHE=/tmp/gocache
GENERATE_CODE_LIST={$$(go list ./... | grep -e apis/crd -e apis/runtime | paste -s -d, -)}
GENERATE_MANIFEST_LIST={$$(go list ./... | grep apis/crd | paste -s -d, -)}

DOCKERIZE := \
	 docker run --rm -u $$(id -u):$$(id -g) \
		-e "GOPATH=$(DOCKER_GOPATH)" \
		-e "GOCACHE=$(DOCKER_GOCACHE)" \
		-e "GOLANGCI_LINT_CACHE=/tmp/.cache" \
		-v $(shell go env GOPATH):$(DOCKER_GOPATH):rw \
		-v $(shell go env GOCACHE):$(DOCKER_GOCACHE):rw \
		-v $(CURDIR):$(DOCKER_SRC) \
		-w $(DOCKER_SRC) \
		$(BUILDER_IMG)

all: build

# Build binaries
build-bin: docker-builder generate tidy
	$(DOCKERIZE) hack/build-bin.sh

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
#deploy: manifests
#	cd config/manager && kustomize edit set image controller=${IMG}
#	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: docker-builder
	$(DOCKERIZE) controller-gen $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths=$(GENERATE_MANIFEST_LIST) output:crd:artifacts:config=config/crd/bases
	$(DOCKERIZE) kustomize build config/default > ./config/nephe.yml

mock: docker-builder
	$(DOCKERIZE) hack/mockgen.sh

# Run unit-tests
unit-test: mock
	$(DOCKERIZE) go test -v -cover -count 1 $$(go list antrea.io/nephe/pkg/...) --ginkgo.v

# Run lint against code
golangci-lint: docker-builder
	$(DOCKERIZE)  golangci-lint run --timeout 10m

# Run go mod tidy against code
tidy: docker-builder
	rm -f go.sum
	$(DOCKERIZE) go mod tidy

.PHONY: check-copyright
check-copyright:
	hack/add-license.sh

.PHONY: add-copyright
add-copyright:
	hack/add-license.sh --add

.PHONY: toc
toc:
	echo "===> Generating Table of Contents for Nephe docs <==="
	$(CURDIR)/hack/update-toc.sh

.PHONY: verify
verify:
	@echo "===> Verifying spellings <==="
	GO=$(GO) $(CURDIR)/hack/verify-spelling.sh
	@echo "===> Verifying Table of Contents <==="
	GO=$(GO) $(CURDIR)/hack/verify-toc.sh
	@echo "===> Verifying documentation formatting for website <==="
	$(CURDIR)/hack/verify-docs-for-website.sh

# Generate code
generate: docker-builder
	$(DOCKERIZE) controller-gen object:headerFile="hack/boilerplate.go.txt" paths=$(GENERATE_CODE_LIST)

# Build the product images
build: build-bin
	hack/build-product.sh

# Push the docker image
docker-push:
	docker push ${IMG}

# create docker container builder
docker-builder:
ifeq (, $(shell docker images -q $(BUILDER_IMG) ))
	docker build --target builder -f ./build/images/Dockerfile -t $(BUILDER_IMG) .
endif

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN): ## Ensure that the directory exists
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.8.0

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE):
	echo kustomizecall
	curl -s $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN)

.PHONY: controller-gen
	controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN):
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

# Run integration-tests
integration-test-aws:
	ginkgo -v --failFast --focus=".*test-aws.*" test/integration/ -- \
	    -manifest-path=../../config/nephe.yml -preserve-setup-on-fail=true -cloud-provider=AWS

integration-test-azure:
	ginkgo -v --failFast --focus=".*test-azure.*" test/integration/ -- \
        -manifest-path=../../config/nephe.yml -preserve-setup-on-fail=true -cloud-provider=Azure
