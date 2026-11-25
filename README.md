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

## 重力余数补偿与冲量守恒

重力按子步分摊时，整除余数不能丢。`sim.GravitySchedule` 用欧几里得分解
`G = q·S + ρ`，把 `ρ` 拆成 `ρ` 个 `q+1`，使每帧 `S` 个子步的重力冲量之和
**逐帧严格等于设定值 `G`**（不是只在末帧对上）。完整证明、跨帧余数台账的
误差界、三档子步数（3/5/7）×1000 帧的逐帧零差值证据，以及两档帧数
（100/1000）的哈希，见 `artifacts/impulse/REMAINDER_PROOF.md`。

```
make lab     # 重新生成 artifacts/impulse/* 与 artifacts/hashes_F*.txt
```

