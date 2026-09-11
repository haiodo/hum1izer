BIN     := hum1izer
PKG     := ./cmd/hum1izer
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test fmt vet lint check install skills clean

all: check build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

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
