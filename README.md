# goframe

确定性帧同步内核，用于回放与状态对拍。

- `api/` 对外入口，`Options` 是参数声明处
- `internal/fixed/` Q16.16 定点数，避免浮点差异
- `internal/sim/` 积分内核，只做加法与定点乘法
- `internal/hash/` 状态指纹，按固定顺序混合
- `predict/` 客户端本地预测 + 服务器权威纠正：环形重放缓冲、回放纠正与硬对齐
- `replay/` 记录与回放
- `cmd/goframe/` 命令行，`run` 子命令打印每帧指纹并自检回放一致
- `cmd/predictlab/` 预测/纠正实验，三档提前量输出逐帧轨迹 CSV 与汇总 JSON
- `tools/plot_trajectories.py` 由轨迹 CSV 渲染轨迹图（需 venv 内 matplotlib）

```
make test
go run ./cmd/goframe run --frames 20 --bodies 8 --substeps 4 --gravity 2
go run ./cmd/predictlab --ticks 120 --out artifacts/lab
.venv/bin/python -I tools/plot_trajectories.py artifacts/lab artifacts/figures
```

预测/纠正的收敛条件（重放帧数 R ≥ 提前量 N）、帧数计算与逐档实测见
`artifacts/REPORT.md`。
