VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -ldflags "-s -w \
	-X main.version=$(VERSION) \
	-X main.gitCommit=$(GIT_COMMIT) \
	-X main.buildTime=$(BUILD_TIME)"

BINARY_NAME := pb2s3

.PHONY: build
build: build-x86

.PHONY: build-all
build-all: build-x86 build-arm64 build-arm32 build-arm32v6

.PHONY: build-x86
build-x86:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY_NAME)-x86

.PHONY: build-arm64
build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-arm64

# For Raspberry Pi 2/3/4 (32-bit OS)
.PHONY: build-arm32
build-arm32:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build $(LDFLAGS) -o $(BINARY_NAME)-arm32

# For Raspberry Pi 1/Zero/Zero W (ARMv6)
.PHONY: build-arm32v6
build-arm32v6:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build $(LDFLAGS) -o $(BINARY_NAME)-arm32v6

.PHONY: clean
clean:
	rm -f $(BINARY_NAME)-x86 $(BINARY_NAME)-arm64 $(BINARY_NAME)-arm32 $(BINARY_NAME)-arm32v6

.PHONY: test
test:
	go test -v ./...

.PHONY: lint
lint:
	go vet ./...
	@command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed, skipping"

.PHONY: release
release: clean build-all
	@echo "Built binaries:"
	@ls -la $(BINARY_NAME)-*
