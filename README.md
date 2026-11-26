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

## 快照漂移定位（字段顺序 × 字节序）

旧版 v1 快照为「大端 + 整段字段反转」；未迁移的读取路径按「小端 + 不反转」
解释时，第 k 个字段得到 `bswap32(c[N-1-k])`，导致回放漂移。完整证明、三份
快照、逐帧比对与漂移量公式见 `artifacts/REPORT.md`。

```
go run ./cmd/goframe drift  --out artifacts  # 真实内核重跑并生成快照/表格/JSON
go run ./cmd/goframe verify --out artifacts  # 从磁盘复核规范化后逐帧哈希==直跑
go test ./...                                # 含漂移帧、f·N 公式、S 子步断言
```

新增代码：`internal/snapfmt/`（v1/v2 编解码与 2×2 解释）、`internal/drift/`
（实验、公式、产物与磁盘复核）、`api.Restore`/`sim.State.SetRaw`（快照恢复入口）。
