.PHONY: build test install clean release cross

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -s -w -X main.version=$(VERSION)

# Build zinc compiler
build:
	go build -ldflags "$(LDFLAGS)" -o zinc ./cmd/zinc/

# Run all e2e tests
test:
	bash run_e2e.sh

# Install to /usr/local/bin (or DESTDIR)
install: build
	install -m 755 zinc $(DESTDIR)/usr/local/bin/zinc

# Cross-compile for all platforms
cross: build
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/zinc-linux-amd64   ./cmd/zinc/
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/zinc-linux-arm64   ./cmd/zinc/
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/zinc-darwin-amd64  ./cmd/zinc/
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/zinc-darwin-arm64  ./cmd/zinc/
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/zinc-windows-amd64.exe ./cmd/zinc/
	@echo "Built all platforms in dist/"

# Release via goreleaser (requires GITHUB_TOKEN)
release:
	goreleaser release --clean

# Clean build artifacts
clean:
	rm -f zinc
	rm -rf dist/ zinc-out/
