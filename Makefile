IMAGE_NAME ?= cm-sandbox
TAG ?= latest
USER_UID ?= $(shell id -u)
USER_GID ?= $(shell id -g)

.PHONY: help build clean

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build the CodeMender interactive sandbox Docker image
	docker build \
		--build-arg USER_UID=$(USER_UID) \
		--build-arg USER_GID=$(USER_GID) \
		-t $(IMAGE_NAME):$(TAG) \
		-f docker/Dockerfile .

clean: ## Remove the sandbox Docker image
	docker rmi -f $(IMAGE_NAME):$(TAG) || true
