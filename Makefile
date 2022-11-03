
# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,crdVersions=v1"

run-with-cleanup = $(1) && $(2) || (ret=$$?; $(2) && exit $$ret)

all: manager

# Run tests
test: generate vet manifests
	go test ./api/... ./controllers/... ./internal/... -coverprofile cover.out

# Start KIND pseudo-cluster
kind-start:
	GO111MODULE=on go get "sigs.k8s.io/kind@v0.11.1" && kind create cluster

# Stop KIND pseudo-cluster
kind-stop:
	GO111MODULE=on go get "sigs.k8s.io/kind@v0.11.1" && kind delete cluster

# Deploy on KIND
# Ensures the controller image is built, deploys the image to KIND cluster along with necessary configuration
kind-deploy: manager manifests docker-build-notest kind-start
	kubectl config set-context kind-kind
	kind load docker-image controller:latest
	kubectl apply -f config/crd/bases
	kustomize build config/default | kubectl apply -f -

# private
kind-test: kind-deploy
	kubectl config set-context kind-kind
	go get github.com/onsi/ginkgo/ginkgo
	ginkgo -v ./tests/integration/...

# Run integration tests on local KIND cluster
test-integration:
	$(call run-with-cleanup, $(MAKE) kind-test, $(MAKE) kind-stop)

# Build manager binary
manager: generate vet
	CGO_ENABLED=0 GO111MODULE=on GOOS=linux GOARCH=amd64 go build -a -o manager main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate vet
	go run ./main.go

# Install CRDs into a cluster
install: manifests
	kubectl apply -f config/crd/bases

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	kubectl apply -f config/crd/bases
	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Format the source code
format: .bin/ory node_modules
	.bin/ory dev headers license --exclude=api/v1alpha1/zz_generated.deepcopy.go
	go fmt ./...
	npm exec -- prettier --write .

licenses: .bin/licenses node_modules  # checks open-source licenses
	.bin/licenses

.bin/licenses: Makefile
	curl https://raw.githubusercontent.com/ory/ci/master/licenses/install | sh

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths=./api/...

# Build the docker image
docker-build-notest:
	docker build . -t ${IMG}
	@echo "updating kustomize image patch file for manager resource"
	sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/default/manager_image_patch.yaml

docker-build: test docker-build-notest

# Push the docker image
docker-push:
	docker push ${IMG}

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.5.0
CONTROLLER_GEN=$(shell which controller-gen)
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

.bin/ory: Makefile
	curl https://raw.githubusercontent.com/ory/meta/master/install.sh | bash -s -- -b .bin ory v0.1.47
	touch .bin/ory

node_modules: package-lock.json
	npm ci
	touch node_modules
