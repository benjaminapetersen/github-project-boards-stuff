.PHONY: all build clean clean-cache kube-board tidy vet fmt test help

BINDIR := bin
CACHEDIR := .cache

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
