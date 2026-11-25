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

## 重力 9.8 的定点精度实验

9.8=49/5 在二进制定点下只能近似（Q16.16 分子 642253，δg=+1/327680）。
该误差每帧贡献恒定加速度差，两次积分后位移差为 `δg·n(n+1)/2`，随帧数平方增长。
完整证明、系数、帧限公式与“三档分子位数 × 两条抛物轨迹”的逐行对照表见
[`docs/gravity-precision.md`](docs/gravity-precision.md)。

```
go run ./cmd/projectile                # 生成 artifacts/projectile_{tables.md,metrics.json,traces.csv}
python3 -I analysis/verify_gravity.py  # 独立复核比例、越界帧与容差判定
```

三档（ε=0.1）首次越界帧为 **16 / 256 / 65536**；高抛轨迹 B（408 帧落地）在
Q16.16 下落点差 0.2546>0.1 不过，升到 Q32.32 后为 3.9e-6 通过。
`Options.GravityNum/GravityDen`（如 `49/5`）提供精确分数重力入口，零值向后兼容。
