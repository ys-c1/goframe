GO ?= go
.PHONY: all test vet lab

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

# 生成余数/冲量守恒的全部证据产物到 artifacts/（生成器自带零差值闸门）。
lab:
	mkdir -p artifacts/impulse
	$(GO) run ./cmd/impulselab | tee artifacts/impulse/verification_summary.txt
