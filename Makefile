BINARY := recoil
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PKG := github.com/1broseidon/recoil/cmd
LDFLAGS := -X $(VERSION_PKG).version=$(VERSION) -X $(VERSION_PKG).commit=$(COMMIT) -X $(VERSION_PKG).date=$(DATE)

ifneq ($(filter -DSQLITE_ENABLE_FTS5,$(CGO_CFLAGS)),-DSQLITE_ENABLE_FTS5)
override CGO_CFLAGS += -DSQLITE_ENABLE_FTS5
endif
export CGO_CFLAGS
export CGO_ENABLED := 1

.PHONY: build clean install test

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test ./...

clean:
	rm -f $(BINARY)
