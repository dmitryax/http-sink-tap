APP ?= http-sink-tap
TAG ?= latest
IMAGE := $(DOCKER_REGISTRY)/$(APP):$(TAG)
K8S_MANIFEST := k8s/http-sink-tap.yaml

define require_DOCKER_REGISTRY
	@if [ -z "$(DOCKER_REGISTRY)" ]; then \
		echo "DOCKER_REGISTRY is required. Example: make $@ DOCKER_REGISTRY=ghcr.io/example"; \
		exit 1; \
	fi
endef

define require_envsubst
	@command -v envsubst >/dev/null 2>&1 || { \
		echo "envsubst is required to render $(K8S_MANIFEST)"; \
		exit 1; \
	}
endef

define render_k8s_manifest
DOCKER_REGISTRY="$(DOCKER_REGISTRY)" APP="$(APP)" TAG="$(TAG)" envsubst '$$DOCKER_REGISTRY $$APP $$TAG' < $(K8S_MANIFEST)
endef

.PHONY: lint test run docker-build docker-push k8s-manifest deploy port-forward

lint:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "Files need gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	go vet ./...

test:
	go test ./...

run:
	go run .

docker-build:
	$(call require_DOCKER_REGISTRY)
	docker build -t $(IMAGE) .

docker-push:
	$(call require_DOCKER_REGISTRY)
	docker push $(IMAGE)

k8s-manifest:
	$(call require_DOCKER_REGISTRY)
	$(call require_envsubst)
	@$(call render_k8s_manifest)

deploy:
	$(call require_DOCKER_REGISTRY)
	$(call require_envsubst)
	$(call render_k8s_manifest) | kubectl apply -f -

port-forward:
	kubectl -n http-sink-tap port-forward svc/http-sink-tap 8080:8080 8081:8081
