BINARY  := xraya
BUILD   := build
VER     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
FLAGS   := -trimpath -ldflags="-s -w -X main.version=$(VER)"

.PHONY: all linux-amd64 linux-arm64 clean

all: linux-amd64 linux-arm64

linux-amd64:
	@mkdir -p $(BUILD)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(FLAGS) -o $(BUILD)/$(BINARY)-linux-amd64 .
	@echo "✓  $(BUILD)/$(BINARY)-linux-amd64"

linux-arm64:
	@mkdir -p $(BUILD)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(FLAGS) -o $(BUILD)/$(BINARY)-linux-arm64 .
	@echo "✓  $(BUILD)/$(BINARY)-linux-arm64"

clean:
	rm -rf $(BUILD)
