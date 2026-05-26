APP ?= http-sink-tap
TAG ?= latest
IMAGE := $(DOCKER_REGISTRY)/$(APP):$(TAG)

define require_docker_registry
	@if [ -z "$(DOCKER_REGISTRY)" ]; then \
		echo "DOCKER_REGISTRY is required. Example: make $@ DOCKER_REGISTRY=registry.example.com"; \
		exit 1; \
	fi
endef

.PHONY: test run docker-build docker-push deploy port-forward

test:
	go test ./...

run:
	go run .

docker-build:
	$(call require_docker_registry)
	docker build -t $(IMAGE) .

docker-push:
	$(call require_docker_registry)
	docker push $(IMAGE)

deploy:
	$(call require_docker_registry)
	kubectl apply -f k8s/http-sink-tap.yaml
	kubectl -n http-sink-tap set image deployment/http-sink-tap http-sink-tap=$(IMAGE)

port-forward:
	kubectl -n http-sink-tap port-forward svc/http-sink-tap 8080:8080 8081:8081
