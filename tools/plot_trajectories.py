#!/usr/bin/env python3
"""读取 predictlab 产出的逐帧轨迹 CSV，绘制两条轨迹图：

  1. trajectory_diverging.png —— 条件不满足（N=5, R=1）：客户端预测轨迹
     与服务器权威轨迹持续分离，每次硬对齐产生可见跳跃。
  2. trajectory_converged.png —— 条件满足（N=5, R=5）：两条轨迹重合，
     附残差放大子图，跳跃全程低于 1 像素。

配色与标记遵循 dataviz 规范：蓝 #2a78d6（权威/服务器）、橙 #eb6834（客户端）、
红 #d03b3b 仅作硬对齐事件状态标记；2px 线、8px 标记、表面色描边环。
"""
import csv
import os
import sys

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib import font_manager

# 系统 DejaVu Sans 不含 CJK 字形，显式注册本机 Noto Sans CJK SC。
_CJK_FONT = "/home/ys/.local/share/fonts/NotoSansCJKsc-Regular.ttf"
if os.path.exists(_CJK_FONT):
    font_manager.fontManager.addfont(_CJK_FONT)
    plt.rcParams["font.family"] = ["Noto Sans CJK SC", "DejaVu Sans"]
plt.rcParams["axes.unicode_minus"] = False

SURFACE = "#fcfcfb"
GRID = "#e1e0d9"
INK = "#0b0b0b"
INK2 = "#52514e"
MUTED = "#898781"
BLUE = "#2a78d6"   # 服务器权威轨迹
ORANGE = "#eb6834"  # 客户端预测轨迹
RED = "#d03b3b"     # 硬对齐事件（状态色，带文字标签）

PX_PER_UNIT = 32.0
ONE = 1 << 16


def load(path):
    rows = {"tick": [], "client_px": [], "server_px": [], "mode": [],
            "jump_px": [], "gap_px": []}
    with open(path, newline="", encoding="utf-8") as f:
        for r in csv.DictReader(f):
            rows["tick"].append(int(r["tick"]))
            rows["client_px"].append(int(r["client_pos_raw"]) / ONE * PX_PER_UNIT)
            rows["server_px"].append(int(r["server_pos_raw"]) / ONE * PX_PER_UNIT)
            rows["mode"].append(r["mode"])
            rows["jump_px"].append(float(r["jump_px"]))
            rows["gap_px"].append(float(r["gap_px"]))
    return rows


def style_axes(ax):
    ax.set_facecolor(SURFACE)
    ax.grid(True, color=GRID, linewidth=1.0, zorder=0)
    for spine in ax.spines.values():
        spine.set_color(GRID)
    ax.tick_params(colors=MUTED, labelsize=9)
    ax.xaxis.label.set_color(INK2)
    ax.yaxis.label.set_color(INK2)
    ax.title.set_color(INK)


def plot_diverging(rows, out_path):
    fig, (ax1, ax2) = plt.subplots(
        2, 1, figsize=(10, 8), dpi=150, facecolor=SURFACE,
        gridspec_kw={"height_ratios": [2.2, 1]},
    )
    fig.suptitle("预测轨迹 vs 权威轨迹：重放帧数不足（提前量 N=5，重放 R=1，R<N 不收敛）",
                 fontsize=13, color=INK, fontweight="bold")

    ax1.plot(rows["tick"], rows["server_px"], color=BLUE, linewidth=2.0,
             label="服务器权威轨迹", zorder=3)
    ax1.plot(rows["tick"], rows["client_px"], color=ORANGE, linewidth=2.0,
             label="客户端预测轨迹", zorder=3)

    # 首次硬对齐：整段实验中最大的一次画面跳变
    first_resync = next(i for i, m in enumerate(rows["mode"]) if m == "resync")
    ft = rows["tick"][first_resync]
    fy = rows["client_px"][first_resync]
    ax1.scatter([ft], [fy], s=90, color=RED, zorder=5,
                edgecolors=SURFACE, linewidths=2,
                label="首次硬对齐（最大跳变 %.2f px）" % rows["jump_px"][first_resync])
    ax1.annotate("t=%d 硬对齐跳变 %.2f px" % (ft, rows["jump_px"][first_resync]),
                 (ft, fy), textcoords="offset points", xytext=(14, 26),
                 fontsize=9, color=RED,
                 arrowprops=dict(arrowstyle="->", color=RED, lw=1.2))
    # 稳态偏差带：硬对齐后客户端仍持续落后于权威轨迹
    steady_gap = rows["gap_px"][-1]
    t_end = rows["tick"][-1]
    ax1.annotate("稳态偏差 %.2f px（不收敛）" % steady_gap,
                 (t_end, rows["client_px"][-1]), textcoords="offset points",
                 xytext=(-150, 14), fontsize=9, color=INK2)

    ax1.set_ylabel("X 位置（像素）")
    ax1.legend(loc="upper left", frameon=False, fontsize=9)
    style_axes(ax1)

    # 下图：逐帧位置偏差，展示“大跳一次后偏差长期维持”的不收敛形态
    ax2.plot(rows["tick"], rows["gap_px"], color=RED, linewidth=2.0, zorder=3,
             label="客户端—权威位置偏差")
    ax2.axhline(1.0, color=MUTED, linewidth=1.2, linestyle="--")
    ax2.annotate("1 像素达标线", (rows["tick"][-1], 1.0), textcoords="offset points",
                 xytext=(-58, 4), fontsize=8, color=MUTED)
    ax2.set_xlabel("帧号 tick")
    ax2.set_ylabel("位置偏差（像素）")
    ax2.set_title("逐帧位置偏差：首次硬对齐 %.2f px，之后稳定在 %.2f px，全程高于 1 px"
                  % (max(rows["jump_px"]), steady_gap),
                  fontsize=10, loc="left")
    ax2.legend(loc="center right", frameon=False, fontsize=9)
    style_axes(ax2)

    fig.tight_layout(rect=(0, 0, 1, 0.96))
    fig.savefig(out_path, facecolor=SURFACE)
    plt.close(fig)


def plot_converged(rows, out_path):
    fig, (ax1, ax2) = plt.subplots(
        2, 1, figsize=(10, 8), dpi=150, facecolor=SURFACE,
        gridspec_kw={"height_ratios": [2.2, 1]},
    )
    fig.suptitle("预测轨迹 vs 权威轨迹：按条件取重放帧数（提前量 N=5，重放 R=5，R≥N 收敛）",
                 fontsize=13, color=INK, fontweight="bold")

    ax1.plot(rows["tick"], rows["server_px"], color=BLUE, linewidth=2.0,
             label="服务器权威轨迹", zorder=3)
    ax1.plot(rows["tick"], rows["client_px"], color=ORANGE, linewidth=2.0,
             linestyle="--", label="客户端预测轨迹（回放纠正后重合）", zorder=3)
    ax1.scatter([rows["tick"][-1]], [rows["server_px"][-1]], s=64, color=BLUE,
                zorder=4, edgecolors=SURFACE, linewidths=2)
    ax1.scatter([rows["tick"][-1]], [rows["client_px"][-1]], s=64, color=ORANGE,
                zorder=4, edgecolors=SURFACE, linewidths=2)
    ax1.set_ylabel("X 位置（像素）")
    ax1.legend(loc="upper left", frameon=False, fontsize=9)
    style_axes(ax1)

    # 下图：残差放大到亚像素尺度（自适应纵轴，1px 达标线置于图外上方）
    ax2.plot(rows["tick"], rows["gap_px"], color=ORANGE, linewidth=2.0,
             label="客户端—权威位置偏差", zorder=3)
    # 回放纠正事件用蓝色点标出
    rt = [t for t, m in zip(rows["tick"], rows["mode"]) if m == "replay"]
    rg = [g for g, m in zip(rows["gap_px"], rows["mode"]) if m == "replay"]
    ax2.scatter(rt[::8], rg[::8], s=28, color=BLUE, zorder=4,
                edgecolors=SURFACE, linewidths=1.2, label="回放纠正帧")
    ax2.axhline(1.0, color=MUTED, linewidth=1.2, linestyle="--")
    ax2.annotate("1 像素达标线（残差远低于此线）", (rows["tick"][-1], 1.0),
                 textcoords="offset points", xytext=(-130, 4),
                 fontsize=8, color=MUTED)
    ax2.set_ylim(0, max(0.012, max(rows["gap_px"]) * 1.6))
    ax2.set_xlabel("帧号 tick")
    ax2.set_ylabel("位置偏差（像素）")
    ax2.set_title("残差放大：全程最大偏差 %.4f px，最大回放跳跃 %.4f px，均 < 1 px"
                  % (max(rows["gap_px"]), max(rows["jump_px"])),
                  fontsize=10, loc="left")
    ax2.legend(loc="upper right", frameon=False, fontsize=9)
    style_axes(ax2)

    fig.tight_layout(rect=(0, 0, 1, 0.96))
    fig.savefig(out_path, facecolor=SURFACE)
    plt.close(fig)


def plot_comparison(lab_dir, out_path):
    """三档提前量 × 两种重放帧数的最大跳跃对比（汇总佐证图）。"""
    import json
    with open(os.path.join(lab_dir, "summary.json"), encoding="utf-8") as f:
        summaries = json.load(f)
    # 去重（N=1 两行相同）并按 N、R 排序
    seen, rows = set(), []
    for s in summaries:
        key = (s["lead"], s["replay"])
        if key in seen:
            continue
        seen.add(key)
        rows.append(s)
    rows.sort(key=lambda s: (s["lead"], s["replay"]))

    labels = ["N=%d\nR=%d%s" % (s["lead"], s["replay"],
              " ✓" if s["condition_ge_lead"] else " ✗") for s in rows]
    jumps = [s["max_jump_px"] for s in rows]
    colors = [BLUE if s["converged"] else RED for s in rows]

    fig, ax = plt.subplots(figsize=(10, 5.2), dpi=150, facecolor=SURFACE)
    fig.suptitle("三档提前量逐档比对：最大画面跳跃（120 帧）",
                 fontsize=13, color=INK, fontweight="bold")
    y = range(len(rows))
    bars = ax.barh(list(y), jumps, height=0.55, color=colors, zorder=3)
    ax.set_yticks(list(y))
    ax.set_yticklabels(labels, fontsize=10)
    ax.invert_yaxis()
    ax.axvline(1.0, color=MUTED, linewidth=1.4, linestyle="--")
    ax.text(1.05, -0.75, "1 像素达标线", fontsize=9, color=MUTED)
    for yi, (bar, s) in enumerate(zip(bars, rows)):
        w = bar.get_width()
        ax.text(w + max(jumps) * 0.012, yi,
                "%.4f px %s" % (w, "达标" if s["converged"] else
                                ("不达标·硬对齐×%d" % s["hard_resyncs"])),
                va="center", fontsize=9, color=INK2)
    ax.set_xlabel("全程最大纠正跳跃（像素）")
    ax.set_xlim(0, max(jumps) * 1.28)
    # 图例（颜色含义）
    ax.scatter([], [], color=BLUE, s=70, label="R≥N：回放纠正，跳跃亚像素")
    ax.scatter([], [], color=RED, s=70, label="R<N：硬对齐，跳跃随 N 放大")
    ax.legend(loc="lower right", frameon=False, fontsize=9)
    style_axes(ax)
    fig.tight_layout(rect=(0, 0, 1, 0.95))
    fig.savefig(out_path, facecolor=SURFACE)
    plt.close(fig)


def main():
    lab_dir = sys.argv[1] if len(sys.argv) > 1 else "artifacts/lab"
    out_dir = sys.argv[2] if len(sys.argv) > 2 else "artifacts/figures"
    os.makedirs(out_dir, exist_ok=True)

    bad = load(os.path.join(lab_dir, "traj_n5_r1.csv"))
    good = load(os.path.join(lab_dir, "traj_n5_r5.csv"))

    p1 = os.path.join(out_dir, "trajectory_diverging.png")
    p2 = os.path.join(out_dir, "trajectory_converged.png")
    p3 = os.path.join(out_dir, "jump_comparison.png")
    plot_diverging(bad, p1)
    plot_converged(good, p2)
    plot_comparison(lab_dir, p3)
    print("wrote", p1)
    print("wrote", p2)
    print("wrote", p3)


if __name__ == "__main__":
    main()
