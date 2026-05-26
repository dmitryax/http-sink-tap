IMAGE ?= http-sink-tap:latest

.PHONY: test run docker-build deploy port-forward

test:
	go test ./...

run:
	go run .

docker-build:
	docker build -t $(IMAGE) .

deploy:
	kubectl apply -f k8s/http-sink-tap.yaml
	kubectl -n http-sink-tap set image deployment/http-sink-tap http-sink-tap=$(IMAGE)

port-forward:
	kubectl -n http-sink-tap port-forward svc/http-sink-tap 8080:8080 8081:8081
