GO ?= go
.PHONY: all test vet projectile

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

projectile:
	$(GO) run ./cmd/projectile
	python3 analysis/projectile_lab.py
