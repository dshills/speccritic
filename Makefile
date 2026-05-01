BINARY     := speccritic
WEB_BINARY := speccritic-web
GO         ?= go
PKG        := ./cmd/speccritic
WEB_PKG    := ./cmd/speccritic-web
BIN_DIR    := bin
BIN        := $(BIN_DIR)/$(BINARY)
WEB_BIN    := $(BIN_DIR)/$(WEB_BINARY)
WEB_ADDR   := 127.0.0.1:8080

.DEFAULT_GOAL := help

.PHONY: build build-web build-all install install-web run-web test race lint help

$(BIN_DIR):
	@mkdir -p "$(BIN_DIR)"

## build: Compile the speccritic binary into ./bin
build: | $(BIN_DIR)
	$(GO) build -o "$(BIN)" "$(PKG)"

## build-web: Compile the speccritic-web binary into ./bin
build-web: | $(BIN_DIR)
	$(GO) build -o "$(WEB_BIN)" "$(WEB_PKG)"

## build-all: Compile CLI and web binaries into ./bin
build-all: build build-web

## install: Install the speccritic binary to $GOBIN (or $GOPATH/bin)
install:
	$(GO) install "$(PKG)"

## install-web: Install the speccritic-web binary to $GOBIN (or $GOPATH/bin)
install-web:
	$(GO) install "$(WEB_PKG)"

## run-web: Run the local web UI; override address with WEB_ADDR=127.0.0.1:8081
run-web: build-web
	"$(WEB_BIN)" --addr "$(WEB_ADDR)"

## test: Run the full test suite
test:
	$(GO) test ./...

## race: Run tests with the race detector enabled
race:
	$(GO) test -race ./...

## lint: Run go vet and golangci-lint
lint:
	$(GO) vet ./...
	golangci-lint run ./...

## help: Show available targets
help:
	@awk 'BEGIN {FS = ":.*?## "; printf "Targets:\n"} \
		/^## / { sub(/^## /, ""); split($$0, a, ": "); printf "  %-10s %s\n", a[1], a[2] }' $(MAKEFILE_LIST)
