GO ?= go
.PHONY: all test vet hashlab

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

hashlab:
	$(GO) run ./cmd/hashlab lab
	$(GO) run ./cmd/hashlab forkcheck --delta 64
