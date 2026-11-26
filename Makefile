GO ?= go
.PHONY: all test vet snapcheck

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

vet:
	$(GO) vet ./...

# 生成差分快照三档对照、证明材料与两种快照存档到 artifacts/rollback/
snapcheck:
	$(GO) run ./cmd/snapcheck
