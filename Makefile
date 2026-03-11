.PHONY: all build clean clean-cache kube-board tidy vet fmt test help image kind-load deploy deploy-env deploy-job helm-lint helm-template helm-install helm-upgrade helm-uninstall

BINDIR := bin
CACHEDIR := .cache
IMAGE := kube-board:latest
CHART := deploy/chart/kube-board
RELEASE := kube-board
NAMESPACE := kube-board
KIND_CLUSTER ?=

all: build

help: ## Show this help
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

build: kube-board ## Build all binaries (currently just kube-board)

kube-board: ## Build the kube-board binary into bin/
	go build -o $(BINDIR)/kube-board ./cmd/kube-board

clean: ## Remove build artifacts (bin/)
	rm -rf $(BINDIR)

clean-cache: ## Remove cached API responses (.cache/)
	rm -rf $(CACHEDIR)

tidy: ## Run go mod tidy
	go mod tidy

vet: ## Run go vet on all packages
	go vet ./...

fmt: ## Run go fmt on all packages
	go fmt ./...

test: ## Run all tests
	go test ./...

image: ## Build container image (docker build)
	docker build -t $(IMAGE) .

kind-load: image ## Build image and load into Kind cluster
	kind load docker-image $(IMAGE) $(if $(KIND_CLUSTER),--name $(KIND_CLUSTER),)

deploy: ## Apply plain Kubernetes manifests (namespace + configmap + cronjob)
	kubectl apply -f deploy/namespace.yaml
	kubectl apply -f deploy/configmap.yaml
	kubectl apply -f deploy/cronjob.yaml

deploy-env: ## Read .env file, apply ConfigMap + Secret to cluster (token stays on-cluster only)
	./scripts/deploy-env.sh $(ENV_FILE)

deploy-job: ## Apply namespace + configmap + one-off job
	kubectl apply -f deploy/namespace.yaml
	kubectl apply -f deploy/configmap.yaml
	kubectl apply -f deploy/job.yaml

helm-lint: ## Lint the Helm chart
	helm lint $(CHART)

helm-template: ## Render chart templates locally (dry-run)
	helm template $(RELEASE) $(CHART)

helm-install: ## Install the Helm chart into the cluster
	helm install $(RELEASE) $(CHART) -n $(NAMESPACE) --create-namespace

helm-upgrade: ## Upgrade (or install) the Helm release
	helm upgrade --install $(RELEASE) $(CHART) -n $(NAMESPACE) --create-namespace

helm-uninstall: ## Uninstall the Helm release
	helm uninstall $(RELEASE) -n $(NAMESPACE)
