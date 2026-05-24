VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X github.com/nerveband/airvault/internal/cli.Version=$(VERSION)"

.PHONY: build test install clean

build:
	go build $(LDFLAGS) -o airvault .

test:
	go test ./...

install: build
	./scripts/install.sh

clean:
	rm -f airvault
