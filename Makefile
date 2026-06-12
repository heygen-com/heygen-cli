VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test lint clean generate check-descriptions

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

generate:
	@find gen/ -name '*.go' -delete 2>/dev/null || true
	go run ./codegen/ -spec $(SPEC) -out gen/ -examples codegen/examples/ $(if $(filter 1,$(STRICT)),-strict)
	gofmt -w gen/

# Advisory scan for HTTP-framed generated descriptions that lack a CLI-surface
# override. Prints a report and always exits 0 (never fails CI). Run after a
# codegen resync; author overrides in internal/clidesc for anything flagged.
check-descriptions:
	go run ./checkdescriptions/
