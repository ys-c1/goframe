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

## 定点乘法取整偏置（1000 帧对拍）

`internal/fixed` 同时提供 `MulTrunc`（向零取整，Go `/`）与 `MulFloor`（向下取整，有符号 `>>`）；
注意历史 `Mul` 的注释写“向零”，但 `>>16` 对负数实为向下取整，行为保持不变。

完整推导、逐帧数据、偏差曲线与可复算的界见
[`artifacts/bias/REPORT.md`](artifacts/bias/REPORT.md)。一键复现：

```
make bias     # 生成 artifacts/bias 下 CSV、summary.json 与唯一一份 frame_hashes.txt
make plot     # 绘制 deviation_curves.png / bounded_oscillation.png
make verify   # 独立复核整数恒等式、有界界与全部 1000 个哈希
```

