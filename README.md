# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `replay/` 记录与回放
- `locate/` 首个分歧帧定位：线性扫描与二分查找两种实现，均计哈希比较次数
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致，
  `locate` 子命令注入一次分歧并对照两种定位、把定位日志写入 `artifacts/`

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
go run ./cmd/goframe locate --frames 1000 --diverge 327
```

## 分歧定位的两个结论

1. 首次哈希不同必定出现在首个状态不同的帧。指纹是 `(tick, 状态字节)` 的
   纯函数（哈希不同则状态必不同）；`Step` 是 `(当前状态, 本帧指令)` 的纯
   函数，同一份日志下状态相同会逐帧延续，故“相等前缀 + 分歧后缀”单调，
   两个“首个不同帧”重合（碰撞可忽略，`Snapshot` 可逐字节兜底）。
2. 二分比较次数上界为 `ceil(log2(N+1))`（见 `locate.Bound`）。100 帧至多
   7 次、1000 帧至多 10 次；线性扫描最坏为 N 次，实测最坏比值 14.29 与
   100.00，与 `N / Bound(N)` 的理论比值完全一致。
