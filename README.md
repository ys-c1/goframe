# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `internal/bucket/` 时间戳→帧号的确定性分桶（欧几里得 floor / 四舍五入对照）
- `replay/` 记录与回放，`TimedLog/BuildLog` 从整数时间戳分桶
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致
- `cmd/bucketlab/` 复现四舍五入分桶分叉并对拍三档帧长，产物在 `artifacts/bucketing/`

## 时间戳分桶

网络时间戳一律以**整数单位**录制，并用向下取整 `frame = floor(t / F)`
（欧几里得除法，余数恒满足 `0 ≤ r < F`）分桶；不要用浮点四舍五入——半帧点
`t = (k+1/2)F` 对相邻两帧等距，平局约定或 1 个 ULP 的重排求和误差就会让同一
时间戳落入相邻帧。证明、三档帧长（8/16/32）的分叉帧号、状态偏差与哈希对比见
[`artifacts/bucketing/REPORT.md`](artifacts/bucketing/REPORT.md)。

```
make test   # 含 floor 唯一性、三档逐帧哈希一致、打乱不变测试
make lab    # 重新生成 artifacts/bucketing/ 下的对比与逐帧哈希
```

```
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
```
