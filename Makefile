GO ?= go
.PHONY: all test vet archive

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

# 重新生成相乘顺序的取证归档（三组数值 + 轨迹对照）。
archive:
	$(GO) run ./cmd/goorder -out artifacts/mul-order
