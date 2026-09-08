BIN ?= $(HOME)/.local/bin

.PHONY: build install test lint

build:
	go build -o spaces .

install:
	mkdir -p $(BIN)
	go build -o $(BIN)/spaces .

test:
	go test ./...

lint:
	test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
