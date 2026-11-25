# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处；`StepWith` 对指令与实体间交互按实体编号全序键规范化
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法；`Shove` 是读源速度、写目标速度的实体间交互
- `internal/hash/` 状态指纹，按固定顺序混合
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致
- `cmd/orderlab/` 积分顺序实验：穷举调用顺序，输出冲量差、唯一性判定、末态哈希与逐步轨迹
- `docs/order-independence.md` 顺序影响末态的证明、唯一性充要条件与反例

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
go run ./cmd/orderlab   # 产物落在 artifacts/orderlab/
```
