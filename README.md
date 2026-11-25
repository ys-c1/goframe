# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `internal/parallel/` 并发步进内核：写入集合分析、确定性交错复现、goroutine 屏障执行
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致；
  `parallelcheck` 子命令验证并发/串行等价条件与差异上界
- `docs/parallel-equivalence.md` 两个定理的形式化证明（等价条件、差异上界）

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
go run ./cmd/goframe parallelcheck
```

`parallelcheck` 输出三栏对拍（并发度 / 末态哈希 / 差异与条件判定）、
差异上界验证、按字段的共享写入统计，并把完整日志写入
`artifacts/parallel_check.log`。
