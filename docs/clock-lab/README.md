# clock-lab 数据快照

这里是 `go run ./cmd/goframe clocklab` 生成结果的**随仓库快照**，内容与
[`../../artifacts/`](../../artifacts) 下的同名文件一致（数值确定，无时间戳）。
证明与公式见 [`../clock-rollback.md`](../clock-rollback.md)。

- `timeline_r1.csv` / `timeline_r10.csv` / `timeline_r100.csv` —— 三档回拨各一条时间轴。
- `comparison.csv` —— 三档“偏差 × 帧号变化”对照表。
- `mono_wall_diff.csv` —— 单调钟−墙钟差，三档 × 三个点。
- `mono_wall_diff_3points.csv` —— 100 帧档恰好三个点（0 / 1000 / 1000 ms）。
- `clock_report.txt` —— 纯文本验收报告。
- `clock_chart.png` / `.svg` —— 帧号悬崖与钟差阶跃图，由 `python3 lab/plot_clock.py artifacts` 绘制。

重新生成权威产物：

```sh
go run ./cmd/goframe clocklab
python3 lab/plot_clock.py artifacts
cp artifacts/{timeline_r1,timeline_r10,timeline_r100,comparison,mono_wall_diff,mono_wall_diff_3points}.csv \
   artifacts/clock_report.txt artifacts/clock_chart.png artifacts/clock_chart.svg docs/clock-lab/
```
