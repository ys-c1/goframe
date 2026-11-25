# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `replay/` 记录与回放
- `verify/` 子步数无关性对拍、`G·h²` 证明数据与硬性测试
- `cmd/goframe/` 命令行，`run` 打印每帧指纹并自检回放一致，`crosscheck` 逐帧比对 1/2/4/8 档
- `cmd/genartifacts/` 实跑并生成 `artifacts/` 下的回放记录与验证证据

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2

# 子步数 1/2/4 同帧哈希一致、1 vs 4 逐帧哈希差值为零、边界档 8：
go run ./cmd/goframe crosscheck --frames 20 --bodies 8 --gravity 2
go test ./verify/ -v
go run ./cmd/genartifacts
```

## 子步数无关性（中点格式）

旧的半隐式 Euler 与子步数相关：重力在第 `k` 个子步产生 `(k+1)·G·h²` 的位置项，
1 档与 4 档相差 `3G/8 = 6·(G·h₄²)`。改用显式中点后，子步项望远镜相消为
`x'=x+v_x`、`y'=y+v_y+G/2`、`v_y'=v_y+G`，不再含子步数 `n`。

定点逐位一致的条件：常量重力、冲量只在帧首、均匀分割、帧内无碰撞/钳制等间断、
不溢出，且子步数为整除定点尺度的 2 的幂（1/2/4/8，上限 16）。实现用 Q(16+16)
宽精度保护位，因此即使冲量不对齐也逐位一致。完整推导、条件与证据见
[`docs/substep-invariance.md`](docs/substep-invariance.md)，回放记录见
[`artifacts/replay.json`](artifacts/replay.json)。
