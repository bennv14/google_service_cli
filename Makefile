BINARY := gsvc
PKG     := github.com/bennv14/google_service_cli/cmd
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X '$(PKG).version=$(VERSION)' -X '$(PKG).commit=$(COMMIT)' -X '$(PKG).date=$(DATE)'
GOBIN   := $(shell go env GOPATH)/bin

.PHONY: build install test tidy clean completions
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .
install:
	go build -ldflags "$(LDFLAGS)" -o $(GOBIN)/$(BINARY) .
test:
	go test ./...
tidy:
	go mod tidy
completions:
	./scripts/completions.sh
clean:
	rm -f $(BINARY)
	rm -rf completions
