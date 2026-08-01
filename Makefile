MODULE := github.com/shiv-eshwar/valve
GO ?= go
DOCKER_GO := docker run --rm -v "$(CURDIR)":/src -w /src golang:1.24

.PHONY: tidy test test-race docker-test docker-test-race

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
