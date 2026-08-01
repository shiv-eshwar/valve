MODULE := github.com/shiv-eshwar/valve
GO ?= go
DOCKER_GO := docker run --rm -v "$(CURDIR)":/src -w /src golang:1.24

.PHONY: tidy test test-race docker-test docker-test-race build-valved compose-up compose-smoke

tidy:
	$(GO) mod tidy

test:
	$(GO) test ./...

test-race:
	$(GO) test ./... -race

docker-test:
	$(DOCKER_GO) go test ./...

docker-test-race:
	$(DOCKER_GO) go test ./... -race

build-valved:
	$(GO) build -o bin/valved ./cmd/valved

compose-up:
	docker compose up -d --build

compose-smoke: compose-up
	@for i in 1 2 3 4 5 6 7 8 9 10; do curl -sf http://127.0.0.1:8080/healthz && break; sleep 2; done
	curl -sf -X POST http://127.0.0.1:8080/v1/check \
	  -H 'Content-Type: application/json' \
	  -d '{"key":{"subject":"local","model":"m"},"limits":{"requests_per_minute":60,"tokens_per_minute":90000},"cost":{"requests":1,"tokens":10}}'
	@echo
	curl -sf http://127.0.0.1:8080/metrics | head -n 20
