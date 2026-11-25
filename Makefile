GO ?= go
.PHONY: all test vet quant

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

## quant 生成量化漂移证明、三档漂移/体积并排表与两版日志原件到 artifacts/
quant:
	$(GO) run ./cmd/goframe quant --frames 20 --bodies 512 --substeps 4 --cap-units 8 --out artifacts > artifacts/quant_report.txt
	cat artifacts/quant_report.txt
