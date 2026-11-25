GO ?= go
.PHONY: all test vet parallelcheck

all: test

test:
	$(GO) vet ./...
	$(GO) test -race ./...

parallelcheck:
	$(GO) run ./cmd/goframe parallelcheck
