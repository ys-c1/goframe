GO ?= go
.PHONY: all test vet bias plot verify

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

# 生成偏置实验的逐帧 CSV / summary / 唯一逐帧哈希
bias:
	$(GO) run ./cmd/biaslab --frames 1000

# 由 CSV 绘制偏差曲线与有界振荡图
plot:
	python3 lab/plot.py artifacts/bias

# 独立复核整数恒等式、有界界与全部哈希
verify:
	python3 -I lab/verify.py artifacts/bias
