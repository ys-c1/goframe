GO ?= go
.PHONY: all test vet perturb

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

vet:
	$(GO) vet ./...

# 生成随机扰动复现实验的两版序列、三栏指标与两进程证据到 artifacts/perturb/
perturb:
	bash scripts/perturb_experiment.sh
