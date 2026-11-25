# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `internal/orderlab/` 相乘顺序实验台，只用于对拍与取证
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致
- `cmd/goorder/` 归档相乘顺序的三组数值与轨迹
- `docs/mul-associativity.md` 定点乘法不结合的证明与顺序约定

## 相乘顺序约定

Q16.16 乘法不满足结合律，`(v*k)*dt` 与 `v*(k*dt)` 最多差
`ceil((|v|+|dt|)/2^16)` 个原始单位。**三因子乘积一律写成 `v*(k*dt)`**
（`fixed.Mul3`，右结合、系数对先行），推导见 `docs/mul-associativity.md`。
不要改成 `v.Mul(k).Mul(dt)`；改动须重算差界、归档与回放指纹基线。

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
```
