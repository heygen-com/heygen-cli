VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test lint clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/heygen ./cmd/heygen/

install:
	go build -ldflags "$(LDFLAGS)" -o $(shell go env GOPATH)/bin/heygen-dev ./cmd/heygen/

test:
	go test ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
