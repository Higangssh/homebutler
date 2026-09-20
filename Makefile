VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.buildDate=$(BUILD_DATE)

.PHONY: build clean test build-web build-all deps-update

build:
	go build -ldflags "$(LDFLAGS)" -o homebutler .

# `npm ci`, never `npm install`: install resolves the ranges again and can
# rewrite package-lock.json, and the release runs this before GoReleaser, which
# refuses to build from a dirty tree. `ci` only reads the lockfile, and fails
# when it disagrees with package.json — which is the answer you want here.
build-web:
	cd web && npm ci && npm run build
	rm -rf internal/server/web_dist/*
	cp -r web/dist/* internal/server/web_dist/

build-all: build-web build

# Raising a dependency is a deliberate act with a lockfile diff to review, not
# something a build does on the way past.
deps-update:
	cd web && npm install

clean:
	rm -f homebutler
	rm -rf web/dist web/node_modules

test:
	go test ./...
