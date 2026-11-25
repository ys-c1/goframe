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

## 随机扰动的回放复现

扰动不能走 `math/rand` 顶层全局源：它在不同进程里随机播种，且某个实体的取值依赖
此前全源被调用的次数（遍历顺序、随机分支都会让序列错位）。

本仓库的做法是按实体编号派生计数器子序列（见 `internal/rng/rng.go`）：

```
u = rng.Derive(masterSeed, entityID, purposeTag, counter)
```

纯 `uint64` + SplitMix64 雪崩混合，无共享游标，因此与进程、调用次序、调用次数无关；
主种子放在 `Options.Seed` 中，随 `replay.Log.Options` 一起记录。`Options.PerturbAmp`
控制每帧速度扰动幅度（Q16.16 原始单位，0 关闭，默认不改变旧行为）：

```
go run ./cmd/goframe run --frames 20 --bodies 8 --perturb 16384 --seed 20261126
```

两版序列存档、两进程逐项一致证据、三档实体数（8/64/512）的
“序列一致性 · 派生开销 · 差异分”三栏表与推导说明见
[`artifacts/perturb/report.md`](artifacts/perturb/report.md)。重新生成全部证据：

```
make perturb
```
