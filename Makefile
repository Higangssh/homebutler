VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.buildDate=$(BUILD_DATE)

.PHONY: build clean test

build:
	go build -ldflags "$(LDFLAGS)" -o homebutler .

clean:
	rm -f homebutler

test:
	go test ./...
