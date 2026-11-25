# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `internal/clock/` 墙钟回拨模型、单调钟 + 帧号自增调度、累计偏差与上界
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致；`clocklab` 子命令生成回拨对照产物

系统时钟回拨为什么让墙钟推导的帧号非单调（累计偏差 = 回拨幅度之和）、
单调钟 + 帧号自增如何把偏差压到一帧以内，证明与差值上界见
[`docs/clock-rollback.md`](docs/clock-rollback.md)。

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
make lab   # 等价于 go run ./cmd/goframe clocklab，生成 artifacts/ 下三条时间轴与对照表
```
