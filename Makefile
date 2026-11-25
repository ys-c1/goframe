GO ?= go
.PHONY: all test vet report verify

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

vet:
	$(GO) vet ./...

## report: 重新生成重力定点精度实验产物
report:
	$(GO) run ./cmd/projectile

## verify: 用标准库 Python 从 JSON 独立核对比例、越界帧与判定
verify:
	python3 -I analysis/verify_gravity.py
