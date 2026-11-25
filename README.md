# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `internal/clock/` 整数毫秒帧调度，累加余数分配长短帧，偏差严格小于一帧
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致，`clock` 子命令生成帧长偏差报告与时间轴对照

帧长量化误差的线性累积证明、余数分配算法与偏差界见 `docs/frame_drift.md`。

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
go run ./cmd/goframe clock --rates 60,30,120 --frames 60,300
```
