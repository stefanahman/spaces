BIN ?= $(HOME)/.local/bin

.PHONY: build install test lint

build:
	go build -o tmux-spaces .

install:
	mkdir -p $(BIN)
	go build -o $(BIN)/tmux-spaces .

test:
	go test ./...

lint:
	test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
