MODULE := github.com/baidubce/bce-cli
VERSION_PKG := $(MODULE)/internal/version

VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILT    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -X '$(VERSION_PKG).Version=$(VERSION)' \
           -X '$(VERSION_PKG).Commit=$(COMMIT)' \
           -X '$(VERSION_PKG).BuildTime=$(BUILT)'

.PHONY: build clean

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bce .

clean:
	rm -f bce bce.exe
