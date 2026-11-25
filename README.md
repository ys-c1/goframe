# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致

## 状态指纹

每个 int32 定点字段按**完整 32 位**参与 FNV-1a 混合，混合前不做任何截断；
int32→uint64 是无损单射，保证"低位相同、高位不同"的分叉状态得到不同指纹。
碰撞证明、构造条件、"只加宽位宽为何不够"的反例及三组构造对照见
[`docs/hash-collision-proof.md`](docs/hash-collision-proof.md)，
构造状态对留档于 `internal/hash/testdata/`。

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
```
