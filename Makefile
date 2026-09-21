BINARY := recoil
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PKG := github.com/1broseidon/recoil/cmd
LDFLAGS := -X $(VERSION_PKG).version=$(VERSION) -X $(VERSION_PKG).commit=$(COMMIT) -X $(VERSION_PKG).date=$(DATE)

# SQLite FTS5 is mandatory and only comes in through CGO. Every target below
# inherits these, so `make test` and `make build` never produce a binary or a
# test run without it.
ifneq ($(filter -DSQLITE_ENABLE_FTS5,$(CGO_CFLAGS)),-DSQLITE_ENABLE_FTS5)
override CGO_CFLAGS += -DSQLITE_ENABLE_FTS5
endif
export CGO_CFLAGS
export CGO_ENABLED := 1

.PHONY: bench build build-check ci clean install lint stress test vulncheck

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

build-check:
	go build ./...

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test ./...

lint:
	golangci-lint run

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

ci: build-check lint test vulncheck

bench:
	go test -run '^$$' -bench 'BenchmarkStore.*10K' -benchmem -benchtime=50x -count=1 ./internal/store

stress: build
	@for fixture in eval/corpora/*/cases.jsonl; do \
		echo "== $$fixture =="; \
		./$(BINARY) eval $$fixture --retrieval fts || exit $$?; \
	done

clean:
	rm -f $(BINARY)
