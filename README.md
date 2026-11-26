# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `internal/pack/` 按位打包快照：保留高 B 位、紧凑比特流与同量纲还原
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致，`snap` 子命令生成三档位宽快照与误差对照

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
go run ./cmd/goframe snap --out artifacts/snapshots
python3 -I scripts/verify_snapshots.py artifacts/snapshots
```

`snap` 的误差界与位宽反解证明见 `artifacts/snapshots/analysis.md`。
