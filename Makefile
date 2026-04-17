BINARY := speccritic
PKG    := ./cmd/speccritic
BIN    := bin/$(BINARY)

.DEFAULT_GOAL := help

.PHONY: build install test race lint help

## build: Compile the speccritic binary into ./bin
build:
	@mkdir -p bin
	go build -o $(BIN) $(PKG)

## install: Install the speccritic binary to $GOBIN (or $GOPATH/bin)
install:
	go install $(PKG)

## test: Run the full test suite
test:
	go test ./...

## race: Run tests with the race detector enabled
race:
	go test -race ./...

## lint: Run go vet and golangci-lint
lint:
	go vet ./...
	golangci-lint run ./...

## help: Show available targets
help:
	@awk 'BEGIN {FS = ":.*?## "; printf "Targets:\n"} \
		/^## / { sub(/^## /, ""); split($$0, a, ": "); printf "  %-10s %s\n", a[1], a[2] }' $(MAKEFILE_LIST)
