GO ?= go
.PHONY: all test vet lab chart

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

vet:
	$(GO) vet ./...

lab:
	$(GO) run ./cmd/goframe clocklab

chart:
	python3 lab/plot_clock.py artifacts
