# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致，
  `audit` 子命令输出同一 tick 重复指令的去重三栏留档（见 `docs/dedup.md`）

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
go run ./cmd/goframe audit
```

同一 tick 重复指令的去重结论（充要条件、丢量公式、开销估算）见
[`docs/dedup.md`](docs/dedup.md)。
