# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法；实体以全局单调递增、永不复用的编号稀疏存放
- `internal/hash/` 状态指纹，按编号升序混合且编号入流，与增删顺序无关
- `internal/alloc/` 编号分配策略对照实验：槽位复用引擎 vs 单调编号引擎
- `replay/` 记录与回放
- `cmd/goframe/` 命令行

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
go run ./cmd/goframe reuse          # 对照实验，日志留档 artifacts/id-reuse.log
```

## 编号空间上界与耗尽策略

上界 `sim.MaxID = 331,776,000`，与可回放最长时长挂钩：

```
60 帧/秒 × 86,400 秒（连续 24 小时）× 每帧至多 64 次生成 = 331,776,000 < 2^31
```

即一个最长回放段（24 小时、每帧 64 个新实体）内 int32 编号不会耗尽。
耗尽时 `Spawn` 返回 `sim.ErrIDExhausted`，**不回绕、不复用旧编号**；
处理方式：结束当前回放段，滚动开启新段并把编号游标归零，段号混入状态指纹，
段边界写入回放日志。
