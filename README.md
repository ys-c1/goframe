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

斜抛误差实验会生成三档角度（水平、45°、竖直）的对照表与两张轨迹图：

```
make projectile
```

产物位于 `artifacts/projectile_*` 与 `artifacts/trajectory-{before,after}.png`，结论见 `artifacts/PROJECTILE_REPORT.md`。
