.PHONY: all build test lint check install clean

all: check build

build:
	mkdir -p bin
	go build -trimpath -o bin/substrate ./cmd/substrate
	go build -trimpath -o bin/substrate-mcp ./cmd/substrate-mcp

test:
	go test ./...

lint:
	go vet ./...

check: test lint

install:
	go install ./cmd/substrate ./cmd/substrate-mcp

clean:
	rm -rf bin
