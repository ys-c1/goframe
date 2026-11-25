GO ?= go
.PHONY: all test vet

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...
