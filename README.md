# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
```

## 指令量化与漂移预算

把指令冲量量化到指定定点位（步长 \(2^{-k}\)），并给出漂移证明、最大步长反解与日志体积对照：

```
make quant     # 生成 artifacts/ 下的证明、并排表、CSV/JSON 与两版日志原件
```

漂移最坏情况界（\(N\) 帧、帧长系数 \(d=\text{SubSteps}\)、raw 网格步长 \(Q\)）：

\[
D_{\max}=\frac{Q\,d\,N(N+1)}4,\qquad
Q_{\max}=\frac{4\Delta}{d\,N(N+1)}
\]

推导、回代验证与三档（6/8/10 定点位）实测数字见
[`artifacts/quantization-proof.md`](artifacts/quantization-proof.md)。
内核侧也可用 `Options.QuantizeBits` 在 `Step` 时直接量化（默认 0，不改变原行为）。
