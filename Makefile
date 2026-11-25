GO ?= go
.PHONY: all test vet verify artifacts crosscheck

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

vet:
	$(GO) vet ./...

# 子步数 1/2/4/8 逐帧对拍（含 1 vs 4 哈希差值为零专检）。
verify:
	$(GO) test ./verify/ -count=1 -v

crosscheck:
	$(GO) run ./cmd/goframe crosscheck --frames 20 --bodies 8 --gravity 2

# 重新生成 artifacts/ 下的回放记录与验证证据。
artifacts:
	$(GO) run ./cmd/genartifacts
