.PHONY: all build clean clean-cache kube-board tidy vet fmt test help image kind-load deploy helm-lint helm-template helm-install helm-upgrade helm-uninstall

BINDIR := bin
CACHEDIR := .cache
IMAGE := kube-board:latest
CHART := deploy/chart/kube-board
RELEASE := kube-board
NAMESPACE := kube-board

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
	kind load docker-image $(IMAGE)

deploy: ## Apply plain Kubernetes manifests from deploy/
	kubectl apply -f deploy/namespace.yaml
	kubectl apply -f deploy/

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
