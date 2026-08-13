BINARY := tau-tool$(if $(findstring Windows,$(OS)),.exe,)
BIN_DIR := bin
PKG := ./cmd/server

.PHONY: all build run run-http vet fmt test clean

all: build

build:
	go build -o $(BIN_DIR)/$(BINARY) $(PKG)

run: build
	./$(BIN_DIR)/$(BINARY)

run-http: build
	./$(BIN_DIR)/$(BINARY) http --port 8899

vet:
	go vet ./...

fmt:
	go fmt ./...

test:
	go test ./...

clean:
	rm -rf $(BIN_DIR)
