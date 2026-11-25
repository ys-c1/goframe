GO ?= go
.PHONY: all test vet lab

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

vet:
	$(GO) vet ./...

lab:
	$(GO) run ./cmd/bucketlab -out artifacts/bucketing
