#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""读取 artifacts/bias 下的精确逐帧 CSV，绘制偏置线性曲线与有界振荡曲线。

只负责可视化；所有数值由 cmd/biaslab 用整数精确运算生成。
用法: python3 lab/plot.py [artifacts/bias]
"""
import csv
import os
import sys
from collections import defaultdict

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib import font_manager

# ---- 中文字体 ----
FONT_PATH = "/home/ys/.local/share/fonts/NotoSansCJKsc-Regular.ttf"
if os.path.exists(FONT_PATH):
    font_manager.fontManager.addfont(FONT_PATH)
    CJK = font_manager.FontProperties(fname=FONT_PATH).get_name()
else:
    CJK = "Noto Sans CJK SC"
plt.rcParams["font.sans-serif"] = [CJK, "Noto Sans CJK SC", "DejaVu Sans"]
plt.rcParams["axes.unicode_minus"] = False

# ---- dataviz reference palette (light, validated) ----
BLUE, ORANGE, AQUA, VIOLET, RED = "#2a78d6", "#eb6834", "#1baf7a", "#6250d6", "#d03b3b"
SURFACE, INK, INK2, MUTED = "#fcfcfb", "#0b0b0b", "#52514e", "#898781"
GRID, BASELINE = "#e1e0d9", "#c3c2b7"

OUT = sys.argv[1] if len(sys.argv) > 1 else "artifacts/bias"


def read_mac():
    curves = defaultdict(list)
    with open(os.path.join(OUT, "bias_frames.csv"), newline="") as fh:
        for row in csv.DictReader(fh):
            curves[(row["case"], row["mode"])].append((int(row["tick"]), float(row["devUlp"])))
    return curves


def read_bounded():
    curves = defaultdict(list)
    with open(os.path.join(OUT, "bounded_frames.csv"), newline="") as fh:
        for row in csv.DictReader(fh):
            curves[row["series"]].append((int(row["tick"]), float(row["devUlp"])))
    return curves


def style_ax(ax):
    ax.set_facecolor(SURFACE)
    ax.grid(axis="y", color=GRID, linewidth=0.8, zorder=0)
    ax.grid(axis="x", color=GRID, linewidth=0.5, alpha=0.6, zorder=0)
    for side in ("top", "right"):
        ax.spines[side].set_visible(False)
    for side in ("left", "bottom"):
        ax.spines[side].set_color(BASELINE)
    ax.tick_params(colors=INK2, labelsize=10)
    ax.xaxis.label.set_color(INK2)
    ax.yaxis.label.set_color(INK2)
    ax.title.set_color(INK)


def end_value(ax, x, y, dy):
    ax.annotate(f"{y:+.1f}", xy=(x, y), xytext=(8, dy), textcoords="offset points",
                va="center", ha="left", fontsize=10.5, color=INK,
                fontweight="bold" if abs(dy) >= 12 else "normal")


def fig_linear(mac):
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(14.5, 6.4), dpi=160)
    fig.patch.set_facecolor(SURFACE)
    nmax = 1000

    # ---- 左：TZ 下 单边 vs 正负各半 ----
    spec1 = [
        # key, color, legend, linewidth, linestyle, 端点竖直偏移
        (("single_pos", "tz"), BLUE, "单边正样本（TZ=floor）", 2.6, "-", -15),
        (("half_asym", "tz"), ORANGE, "正负各半·余数不对称块", 2.6, "-", 8),
        (("half_prng", "tz"), AQUA, "正负各半·随机符号", 1.9, "-", 13),
        (("half_paired", "tz"), MUTED, "正负各半·同模配对", 1.9, "--", -14),
    ]
    for key, color, label, lw, ls, dy in spec1:
        xs = [p[0] for p in mac[key]]
        ys = [p[1] for p in mac[key]]
        ax1.plot(xs, ys, color=color, lw=lw, ls=ls, label=label, zorder=3)
        end_value(ax1, xs[-1], ys[-1], dy)
    ax1.axhline(0, color=BASELINE, lw=1.0, zorder=1)
    ax1.set_xlim(0, nmax * 1.16)
    ax1.set_ylim(-560, 330)
    ax1.set_xlabel("帧 tick")
    ax1.set_ylabel("累计偏差 D$_N$ = 定点累计 − 高精度参照（Q16 最小单位 ULP）")
    ax1.set_title("① 向零取整 TZ：单边 与 “余数不对称的正负各半” 方向相反，均线性增长",
                  fontsize=12.5, fontweight="bold", pad=12)
    ax1.legend(loc="upper left", facecolor=SURFACE, edgecolor=GRID, fontsize=10, framealpha=0.96)
    style_ax(ax1)

    # ---- 右：floor 在同一开环 MAC 中仍然线性负偏 ----
    spec2 = [
        (("single_pos", "floor"), BLUE, "单边正 floor（与 TZ 相同）", 2.4, "-", -16),
        (("half_paired", "floor"), VIOLET, "同模配对 floor：负样本不再回补", 2.2, "-", 15),
        (("half_asym", "floor"), ORANGE, "不对称半 floor", 2.2, "-", 8),
        (("half_paired", "tz"), MUTED, "同模配对 TZ（对照，逐对抵消）", 1.7, "--", -14),
    ]
    for key, color, label, lw, ls, dy in spec2:
        xs = [p[0] for p in mac[key]]
        ys = [p[1] for p in mac[key]]
        ax2.plot(xs, ys, color=color, lw=lw, ls=ls, label=label, zorder=3)
        end_value(ax2, xs[-1], ys[-1], dy)
    ax2.axhline(0, color=BASELINE, lw=1.0, zorder=1)
    ax2.set_xlim(0, nmax * 1.16)
    ax2.set_ylim(-560, 110)
    ax2.set_xlabel("帧 tick")
    ax2.set_ylabel("累计偏差 D$_N$（ULP）")
    ax2.set_title("② 同一开环乘加里，裸 floor 的每帧误差恒负，照样线性累积（不会自动有界）",
                  fontsize=12.5, fontweight="bold", pad=12)
    ax2.legend(loc="lower left", facecolor=SURFACE, edgecolor=GRID, fontsize=10, framealpha=0.96)
    style_ax(ax2)

    fig.suptitle("定点 Q16.16 乘法取整偏置 · 1000 帧 · 系数 C=40503 (c≈0.618027)",
                 fontsize=14, fontweight="bold", color=INK, y=0.995)
    fig.tight_layout(rect=(0, 0, 1, 0.965))
    path = os.path.join(OUT, "deviation_curves.png")
    fig.savefig(path, facecolor=SURFACE, bbox_inches="tight")
    plt.close(fig)
    return path


def fig_bounded(b):
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(14.5, 5.9), dpi=160)
    fig.patch.set_facecolor(SURFACE)

    # ---- 左：残差累加 sawtooth（前 200 帧）----
    xs = [p[0] for p in b["residue_floor"]]
    ys = [p[1] for p in b["residue_floor"]]
    k = 200
    ax1.plot(xs[:k], ys[:k], color=AQUA, lw=1.6, marker="o", ms=2.8,
             markerfacecolor=AQUA, markeredgewidth=0, zorder=3)
    ax1.fill_between(xs[:k], ys[:k], 0, color=AQUA, alpha=0.12, zorder=1)
    ax1.axhline(-1, color=RED, lw=1.6, ls="--", zorder=2)
    ax1.text(3, -0.955, "硬下界 −1 ULP（实际 −1 < D ≤ 0）", color=RED, fontsize=10.5, va="bottom")
    ax1.axhline(0, color=BASELINE, lw=1.2)
    ax1.set_xlim(-3, k)
    ax1.set_ylim(-1.12, 0.18)
    ax1.set_xlabel("帧 tick（仅显示前 200 帧；全程 1000 帧实测 [−0.999, 0]）")
    ax1.set_ylabel("偏差 D$_N$（ULP）")
    ax1.set_title("③ floor + 残差累加：偏差为有界锯齿振荡，界 = 1 ULP（可逐整数复算）",
                  fontsize=12.5, fontweight="bold", pad=12)
    style_ax(ax1)

    # ---- 右：反馈收缩 ----
    series_cfg = [
        ("contract_floor", BLUE, "floor  实测 [−8.79, 0]，末段 2 周期 ≈ −1.50"),
        ("contract_tz", ORANGE, "TZ  实测 [−2.13, +1.45]，末段 2 周期 0.55/1.45"),
    ]
    data = {}
    for series, color, label in series_cfg:
        xs = [p[0] for p in b[series]]
        ys = [p[1] for p in b[series]]
        data[series] = (xs, ys)
        ax2.plot(xs, ys, color=color, lw=1.2, label=label, zorder=3)
    ax2.axhline(15, color=RED, lw=1.5, ls="--", zorder=2)
    ax2.axhline(-15, color=RED, lw=1.5, ls="--", zorder=2)
    ax2.text(12, 15.35, "硬界 +15 ULP", color=RED, fontsize=10.5, va="bottom")
    ax2.text(12, -15.35, "硬界 −15 ULP", color=RED, fontsize=10.5, va="top")
    ax2.axhline(0, color=BASELINE, lw=1.0)
    ax2.set_xlim(0, 1000)
    ax2.set_ylim(-17.5, 17.5)
    ax2.set_xlabel("帧 tick")
    ax2.set_ylabel("偏差 e$_N$（ULP）")
    ax2.legend(loc="upper right", facecolor=SURFACE, edgecolor=GRID, fontsize=9.5, framealpha=0.96)
    ax2.set_title(r"④ 反馈收缩 c=15/16：$|e_N|\leq c^N|e_0|+\Delta(1-c^N)/(1-c)\ \rightarrow\ \Delta/(1-c)=15$ ULP",
                  fontsize=12.5, fontweight="bold", pad=12)
    style_ax(ax2)

    # 末段 2 周期放大
    axins = ax2.inset_axes([0.50, 0.15, 0.47, 0.40])
    lo, hi = 968, 1000
    for series, color, _ in series_cfg:
        xs, ys = data[series]
        seg = [(x, y) for x, y in zip(xs, ys) if lo <= x < hi]
        axins.plot([p[0] for p in seg], [p[1] for p in seg], color=color,
                   lw=1.3, marker="o", ms=3.2, zorder=3)
    axins.axhline(0, color=BASELINE, lw=0.8)
    axins.set_title("末段放大：稳定 2 周期振荡", fontsize=9.5, color=INK2)
    axins.tick_params(labelsize=8, colors=MUTED)
    axins.set_facecolor("#ffffff")
    for side in ("top", "right"):
        axins.spines[side].set_visible(False)
    for side in ("left", "bottom"):
        axins.spines[side].set_color(BASELINE)
    axins.grid(axis="y", color=GRID, lw=0.6)

    fig.suptitle("向下取整获得有界振荡的两种正确结构（裸 floor 开环累加不成立，见图②）",
                 fontsize=14, fontweight="bold", color=INK, y=0.99)
    fig.tight_layout(rect=(0, 0, 1, 0.955))
    path = os.path.join(OUT, "bounded_oscillation.png")
    fig.savefig(path, facecolor=SURFACE, bbox_inches="tight")
    plt.close(fig)
    return path


if __name__ == "__main__":
    p1 = fig_linear(read_mac())
    p2 = fig_bounded(read_bounded())
    print("wrote", p1)
    print("wrote", p2)
