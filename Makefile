GO ?= go
.PHONY: all test vet lab

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

lab:
	$(GO) run ./cmd/orderlab
