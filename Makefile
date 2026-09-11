BIN     := hum1izer
PKG     := .
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/haiodo/hum1izer/internal/cli.Version=$(VERSION)

.PHONY: all build test fmt vet lint check install skills clean

all: check build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) .

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run

check: fmt vet lint test

# Ставит бинарь в GOBIN (по умолчанию ~/go/bin).
install:
	go install -trimpath -ldflags '$(LDFLAGS)' $(PKG)

# Скиллы для всех агентов сразу.
skills: install
	$(BIN) install --all

clean:
	rm -f $(BIN)
