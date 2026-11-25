# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/queue/` 有界指令队列：两种丢弃时机（Immediate/Deferred）× 两种保留策略（TopK/TailDrop）
- `internal/hash/` 状态指纹，按固定顺序混合
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致；`drop-exp` 子命令跑丢弃实验
- `docs/drop-bounds.md` 丢弃偏差上界与保留策略的形式化证明

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
go run ./cmd/goframe drop-exp --out artifacts/drop
```

## 指令丢弃

队列容量不足时按冲量大小（L1 权重）决定保留：

- 偏差上界 = 被丢冲量大小之和 B(D)，与丢弃时机无关（证明见 `docs/drop-bounds.md` 定理 1）
- TopK（保留冲量最大的 K 条）在容量约束下最小化 B(D)（定理 2，交换论证）
- 给定每帧阈值 τ，最小保留容量 `K(τ) = min{k : 尾部最小 n−k 项权重和 ≤ τ}`，
  由 `queue.MinCapacity` 实现；逐帧 K(τ) 保证累计偏差落在总阈值 T·τ 内
- `drop-exp` 在轻/中/重三档负载上对照两种时机与两种策略，
  报告写入 `artifacts/drop/report.md`，逐帧哈希序列写入 `artifacts/drop/hashes-*.txt`
