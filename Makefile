# Governing: REQ-0001, REQ-0006, REQ-0011

IMAGE_NAME ?= cm-runner
TAG ?= latest
# TODO(#19): Validate or resolve container user UID/GID mapping in CI/CD pipeline vs workstation.
# Pre-built registry images have static UIDs, whereas workstation mode requires dynamic host UIDs.
USER_UID ?= $(shell id -u)
USER_GID ?= $(shell id -g)

.PHONY: help fmt lint test coverage build rebuild integration-test clean

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

fmt: ## Format Go source files using goimports and gofmt -s
	@find . -type f -name '*.go' -not -path './vendor/*' | xargs goimports -w
	@find . -type f -name '*.go' -not -path './vendor/*' | xargs gofmt -s -w

lint: fmt ## Format and run Go static analysis (go vet)
	go vet ./...

test: ## Run Go unit tests with race detection and statement coverage check (>= 90%)
	./scripts/check_coverage.sh 90.0

coverage: ## Generate and view HTML test coverage report
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "HTML coverage report generated at coverage.html"

build: lint ## Build the CodeMender batch runner Docker image (always lints first)
	docker build \
		--build-arg USER_UID=$(USER_UID) \
		--build-arg USER_GID=$(USER_GID) \
		-t $(IMAGE_NAME):$(TAG) \
		-f docker/Dockerfile .

rebuild: clean lint ## Clean and rebuild the CodeMender batch runner Docker image from scratch without cache
	docker build \
		--no-cache \
		--build-arg USER_UID=$(USER_UID) \
		--build-arg USER_GID=$(USER_GID) \
		-t $(IMAGE_NAME):$(TAG) \
		-f docker/Dockerfile .

integration-test: ## Run full container verification test suite with timeout
	timeout 60s ./tests/integration_test.sh

clean: ## Remove Docker image and clean local artifacts
	docker rmi -f $(IMAGE_NAME):$(TAG) || true
	rm -f bin/cm-runner.tmp coverage.out coverage.html coverage.txt
