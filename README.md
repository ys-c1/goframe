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

## 状态哈希对拍实验（cmd/hashlab）

`internal/hash` 除全 32 位 FNV（`State`，修复版 v2）外，提供两个窄位变体用于复现漏判：

- `StateDropLow(tick, s, dropBits)`：丢每个字段最低 b 位（分桶量化，桶宽 B=2^b）；
- `StateKeepLow(tick, s, keepBits)`：字面“只取低位”，每字段只保留低 k 位。

实验台命令（产物写入 `artifacts/hashlab/`，两版哈希结果各存一份）：

```
go run ./cmd/hashlab lab                      # 全量：概率矩阵、三组分叉世界、帧对齐
go run ./cmd/hashlab forkcheck --delta 64     # 最小逐帧见证
go test ./...                                 # 概率恒等式与分叉帧回归测试
```

漏判概率（对齐 r=x mod B 在桶内均匀）：

- 丢低位分桶：`P_miss_drop(Δ,b) = max(0, 1-|Δ|/2^b)`，|Δ|≥2^b 时恒为 0；
- 只取低位：`P_miss_keep(Δ,k) = 1[2^k | Δ]`，与对齐无关的 0/1 事件。
