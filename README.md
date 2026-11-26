# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `internal/snap/` 全量/差分快照：字段编号+旧值、定宽编码、逐字段比对、体积上界
- `internal/rollback/` 五帧回滚环，只存差分
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致
- `cmd/snapcheck/` 生成三档改动量对照与快照存档到 `artifacts/rollback/`

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
make snapcheck   # 证明、三行对照表、两种快照存档、最小/最大档验界
```

回滚默认保留五帧：`Step` 自动记录本帧“字段编号 + 帧前旧值”差分，
`World.Rollback` 逐字段写回。可还原的充要条件与体积上界
（N=32 时差分/全量 ≤ 34/23 ≈ 1.478261）见
`artifacts/rollback/PROOF.md`。
