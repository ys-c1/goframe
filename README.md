# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法，并维护逐槽位脏标记
- `internal/hash/` 状态指纹：全量指纹与逐位等价的增量指纹
- `internal/measure/` 两种指纹的耗时、逐位对拍与序列存档
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 打印每帧指纹并自检回放一致，`compare` 做增量/全量对拍

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
go run ./cmd/goframe compare --out artifacts/compare --frames 600
```

## 增量哈希

全量指纹每帧重算全部实体，O(实体数)。增量指纹只重算发生变化的槽位，
O(变化槽位数)，结果与全量逐位相同。定义、等价条件与证明见
`internal/hash/doc.go`；对拍报告写在 `artifacts/compare/summary.md`。

等价条件是“一次更新只影响被更新实体自己的槽位”。sim 的结构满足它：
`Push` 只写目标实体的速度，`Integrate` 的循环体只读写该实体自己的四个值。

不满足时的后果也做了验证：绕过 `State` 方法直接改别的槽位而不标脏，
增量会漏掉那部分影响，差异精确落在被改的槽位上（见
`TestViolationDetectedCrossSlot`）。

## 对拍与计时

`compare` 在三档实体数（64 / 512 / 4096）下跑两种负载：

- `sparse`：无重力，每帧只推 1 个实体，脏槽位数少
- `dense`：有重力，所有实体每帧都在动，脏槽位数等于实体数

每档输出等价判定、差异数、端到端耗时与单帧指纹耗时，并把全量、增量
两版逐帧指纹分别存成 `bodies-<N>-<负载>-full.txt` 与 `-inc.txt`，逐帧
比对存成 `-xor.txt`（xor 恒为零才算等价）。
