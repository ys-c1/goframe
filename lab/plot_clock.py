#!/usr/bin/env python3
"""读取 artifacts/ 下的 clocklab CSV，绘制 2x3 对照图。

上排：回拨点附近的墙钟帧号（橙，悬崖/非单调）vs 单调钟+帧号自增（蓝，严格递增）。
下排：单调钟-墙钟差值阶跃曲线，并标出题目要求的三个点（回拨前/瞬间/后）。

用法：python3 lab/plot_clock.py [artifacts_dir]
输出：<artifacts_dir>/clock_chart.png 与 clock_chart.svg
"""
import csv
import os
import sys

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib import font_manager

# 显式注册 Noto CJK（matplotlib 自己的字体缓存可能扫不到 ~/.local/share/fonts）。
_CJK_CANDIDATES = [
    os.path.expanduser("~/.local/share/fonts/NotoSansCJKsc-Regular.ttf"),
    "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
    "/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
]
CJK_FAMILY = "DejaVu Sans"
for _p in _CJK_CANDIDATES:
    if os.path.exists(_p):
        try:
            font_manager.fontManager.addfont(_p)
            CJK_FAMILY = font_manager.FontProperties(fname=_p).get_name()
            break
        except Exception:
            pass
# 再兜底：让 fontconfig 已知的名字参与匹配。
plt.rcParams["font.sans-serif"] = [CJK_FAMILY, "Noto Sans CJK SC", "DejaVu Sans"]
plt.rcParams["axes.unicode_minus"] = False
plt.rcParams["font.size"] = 10

# 与 dataviz 校验通过的参考色板前两槽一致（light surface）。
BLUE = "#2a78d6"
ORANGE = "#eb6834"
INK = "#0b0b0b"
INK2 = "#52514e"
GRID = "#e7e4dd"
SPINE = "#c9c5bb"
SURFACE = "#fcfcfb"


def read_timeline(path):
    rows = []
    with open(path, newline="", encoding="utf-8") as f:
        for r in csv.DictReader(f):
            rows.append({k: (float(v) if k not in ("phase",) else v) for k, v in r.items()})
    return rows


def read_diff_points(path):
    out = {}
    with open(path, newline="", encoding="utf-8") as f:
        for r in csv.DictReader(f):
            out.setdefault(int(r["rollback_frames"]), []).append(r)
    return out


def main():
    art = sys.argv[1] if len(sys.argv) > 1 else "artifacts"
    levels = [1, 10, 100]
    timelines = {r: read_timeline(os.path.join(art, f"timeline_r{r}.csv")) for r in levels}
    diffs = read_diff_points(os.path.join(art, "mono_wall_diff.csv"))

    fig, axes = plt.subplots(2, 3, figsize=(13.2, 7.4), dpi=160)
    fig.patch.set_facecolor(SURFACE)

    x_lo, x_hi = 96.5, 117.8  # 右侧留出直接标签空间

    for col, R in enumerate(levels):
        rows = timelines[R]
        x = [r["mono_ms"] / 10.0 for r in rows]
        wf = [r["wall_frame"] for r in rows]
        ff = [r["fixed_frame"] for r in rows]

        ax = axes[0][col]
        style_ax(ax)
        ax.plot(x, ff, color=BLUE, lw=2.2, zorder=3, label="单调钟 + 帧号自增")
        ax.plot(x, wf, color=ORANGE, lw=2.2, zorder=4, label="墙钟推导帧号")

        pre = next(r for r in rows if r["phase"] == "rollback_pre")
        hit = next(r for r in rows if r["phase"] == "rollback")
        ax.scatter([pre["mono_ms"] / 10], [pre["wall_frame"]], s=58,
                   color=SURFACE, edgecolor=INK2, linewidth=1.4, zorder=5)
        ax.scatter([hit["mono_ms"] / 10], [hit["wall_frame"]], s=64,
                   color=ORANGE, edgecolor=SURFACE, linewidth=1.6, zorder=6)
        # 跳变量标注（文字用文本色，不借系列色）；按幅度选不压线的位置。
        ymid = (pre["wall_frame"] + hit["wall_frame"]) / 2.0
        label_pos = {1: (108.9, 102.6), 10: (110.6, 80.5), 100: (109.4, 57.0)}[R]
        ax.annotate(f"−{R} 帧", xy=(105, ymid), xytext=label_pos,
                    fontsize=10.5, fontweight="bold", color=INK, va="center",
                    arrowprops=dict(arrowstyle="-|>", color=INK2, lw=1.1,
                                    connectionstyle="arc3,rad=0.12"),
                    bbox=dict(boxstyle="round,pad=0.22", fc=SURFACE, ec=SPINE, lw=0.8))

        # 线端直接标签；R=1 时两线相距仅 1 帧，上下错开避免相撞。
        x_end = 114
        f_end = next(r["fixed_frame"] for r in rows if r["mono_ms"] / 10 == x_end and r["phase"] == "after")
        w_end = next(r["wall_frame"] for r in rows if r["mono_ms"] / 10 == x_end and r["phase"] == "after")
        if R == 1:
            ax.text(x_end + 0.35, f_end + 0.85, "自增", color=INK, fontsize=9.5, va="bottom",
                    bbox=dict(boxstyle="round,pad=0.12", fc=SURFACE, ec="none", alpha=0.85))
            ax.text(x_end + 0.35, w_end - 0.9, "墙钟", color=INK, fontsize=9.5, va="top",
                    bbox=dict(boxstyle="round,pad=0.12", fc=SURFACE, ec="none", alpha=0.85))
        else:
            ax.text(x_end + 0.35, f_end, "自增", color=INK, fontsize=9.5, va="center")
            ax.text(x_end + 0.35, w_end, "墙钟", color=INK, fontsize=9.5, va="center")

        ax.set_xlim(x_lo, x_hi)
        ax.set_title(f"回拨 {R} 帧（{R * 10} ms）", fontsize=12, color=INK, pad=8)
        if col == 0:
            ax.set_ylabel("帧号", color=INK2)
        pad = max(2.2, R * 0.06 + 1.5)
        ax.margins(y=0.16)

        # ---- 下排：钟差曲线 + 三个点 ----
        ax2 = axes[1][col]
        style_ax(ax2)
        gx = [r["mono_ms"] / 10.0 for r in rows]
        gy = [r["gap_ms"] for r in rows]
        ax2.plot(gx, gy, color=ORANGE, lw=2.2, zorder=3)
        pts = diffs[R]
        px = [float(p["mono_ms"]) / 10.0 for p in pts]
        py = [float(p["diff_ms"]) for p in pts]
        ax2.scatter(px, py, s=72, color=ORANGE, edgecolor=SURFACE, linewidth=1.8, zorder=6)
        offsets = [(-10, -17), (8, 12), (10, -2)]
        anchors = ["right", "left", "left"]
        for p, dxdy, ha in zip(pts, offsets, anchors):
            label = f'{p["point"]}: {p["diff_ms"]} ms'
            ax2.annotate(label, xy=(float(p["mono_ms"]) / 10.0, float(p["diff_ms"])),
                         xytext=dxdy, textcoords="offset points", fontsize=9, color=INK,
                         ha=ha, va="center",
                         bbox=dict(boxstyle="round,pad=0.2", fc=SURFACE, ec=SPINE, lw=0.7))
        bound = R * 10
        ax2.axhline(bound, color=INK2, lw=1.0, linestyle=(0, (4, 3)), zorder=2)
        ax2.text(x_lo + 0.4, bound, f"上界 RΔ={bound} ms", ha="left", va="bottom",
                 fontsize=8.8, color=INK2)
        ax2.set_xlim(x_lo, x_hi)
        top = bound * 1.32 + 4
        ax2.set_ylim(-top * 0.18, top)
        ax2.set_xlabel("单调时间（槽，1 槽 = Δ = 10 ms）", color=INK2)
        if col == 0:
            ax2.set_ylabel("钟差 m(t) − w(t)（ms）", color=INK2)

    # 共享图例（≥2 系列必须有图例）。
    handles, labels = axes[0][0].get_legend_handles_labels()
    fig.legend(handles, labels, loc="upper center", ncol=2, frameon=False,
               bbox_to_anchor=(0.5, 0.965), fontsize=11)
    fig.suptitle("系统时钟回拨三档幅度：墙钟帧号非单调悬崖，与单调钟 + 帧号自增的零偏差",
                 fontsize=14, color=INK, y=0.995)
    fig.tight_layout(rect=(0, 0, 1, 0.93), h_pad=2.4, w_pad=1.6)

    png = os.path.join(art, "clock_chart.png")
    svg = os.path.join(art, "clock_chart.svg")
    fig.savefig(png, facecolor=SURFACE)
    fig.savefig(svg, facecolor=SURFACE)
    print("wrote", png, "and", svg)


def style_ax(ax):
    ax.set_facecolor(SURFACE)
    ax.grid(axis="y", color=GRID, lw=0.9)
    ax.set_axisbelow(True)
    for side in ("top", "right"):
        ax.spines[side].set_visible(False)
    for side in ("left", "bottom"):
        ax.spines[side].set_color(SPINE)
    ax.tick_params(colors=INK2, labelsize=9)


if __name__ == "__main__":
    main()
