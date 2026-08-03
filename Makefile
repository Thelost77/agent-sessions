BINARY := agent-sessions
BUILD_DIR := bin
PREFIX ?= $(HOME)/.local
INSTALL_DIR ?= $(PREFIX)/bin

.PHONY: build test check install clean

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/agent-sessions

test:
	go test ./...

check:
	go test ./...
	go test -race ./...
	go vet ./...

install: build
	install -d -m 0755 $(INSTALL_DIR)
	install -m 0755 $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)

clean:
	rm -rf $(BUILD_DIR)
