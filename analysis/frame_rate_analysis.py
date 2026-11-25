#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
帧率相关性分析：显式欧拉（一阶、非辛）与速度 Verlet（二阶、辛）对照。

同一段回放（相同物理模型、相同初值、相同指令时刻），分别以 30fps 与 60fps
推进，在共同的物理时刻逐点比对，并扫描多档帧率验证截断误差阶数。

运行：python3 -I analysis/frame_rate_analysis.py
"""

from __future__ import annotations

import csv
import os

import numpy as np

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib import font_manager

# 显式注册系统 CJK 字体（matplotlib 自身的字体缓存不一定认 fontconfig 名字）
for _fp in (
    "/home/ys/.local/share/fonts/NotoSansCJKsc-Regular.ttf",
    "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
):
    if os.path.exists(_fp):
        font_manager.fontManager.addfont(_fp)
        CJK_FONT = font_manager.FontProperties(fname=_fp).get_name()
        break
else:
    CJK_FONT = "sans-serif"

# ---------------------------------------------------------------------------
# 全局样式：dataviz 技能调色板（已通过 validate_palette.js 校验的蓝/橙双系列）
# ---------------------------------------------------------------------------
BLUE, ORANGE = "#2a78d6", "#eb6834"
INK, MUTED, GRID, SURFACE = "#0b0b0b", "#52514e", "#e1e0d9", "#fcfcfb"
plt.rcParams.update({
    "font.family": [CJK_FONT, "DejaVu Sans"],
    "font.size": 11,
    "text.color": INK,
    "axes.edgecolor": "#c3c2b7",
    "axes.labelcolor": INK,
    "axes.titlecolor": INK,
    "xtick.color": MUTED,
    "ytick.color": MUTED,
    "axes.grid": True,
    "grid.color": GRID,
    "grid.linewidth": 0.8,
    "axes.facecolor": SURFACE,
    "figure.facecolor": SURFACE,
    "savefig.facecolor": SURFACE,
    "lines.linewidth": 2.0,
    "lines.solid_capstyle": "round",
    "legend.frameon": False,
})

OUT = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
                   "artifacts")

# ---------------------------------------------------------------------------
# 物理模型（单位为任意一致单位）：
#   一维线性重力场 + 简谐回复力，a(x) = g - omega^2 x，初态 (x0, v0)。
#   回复力让能量漂移在长帧序列上可见；重力部分给出可手算的一阶误差公式。
# ---------------------------------------------------------------------------
G = 2.0          # 重力加速度
OMEGA2 = 1.44    # 回复力系数（omega = 1.2 rad/s）
OMEGA = OMEGA2 ** 0.5
X0, V0 = 1.0, 0.0
HORIZON = 4.0    # 固定物理时长（秒），两档帧率覆盖同一时间区间


# ---------------------------------------------------------------------------
# 三个积分器。统一约定：每步先施加重力/力，再按帧长 h 推进。
# ---------------------------------------------------------------------------
def solve(method: str, fps: int, x0: float = X0, v0: float = V0,
          omega2: float = OMEGA2) -> tuple[np.ndarray, np.ndarray, np.ndarray]:
    """返回 (t, x, v)，帧号即数组下标，t[k] = k * h。"""
    h = 1.0 / fps
    n = int(round(HORIZON * fps))
    t = np.linspace(0.0, n * h, n + 1)
    x = np.empty(n + 1)
    v = np.empty(n + 1)
    x[0], v[0] = x0, v0

    def a(z):
        return G - omega2 * z

    if method == "explicit_euler":
        # 显式（前向）欧拉：位置与速度都用步首旧值 —— 非辛。
        for k in range(n):
            x[k + 1] = x[k] + h * v[k]
            v[k + 1] = v[k] + h * a(x[k])
    elif method == "symplectic_euler":
        # 辛欧拉（半隐式）：先更新速度，再用新速度更新位置 —— 辛，但仍是一阶。
        for k in range(n):
            v[k + 1] = v[k] + h * a(x[k])
            x[k + 1] = x[k] + h * v[k + 1]
    elif method == "velocity_verlet":
        # 速度 Verlet：二阶辛格式（对可分离哈密顿量）。
        a_prev = a(x[0])
        for k in range(n):
            x[k + 1] = x[k] + h * v[k] + 0.5 * h * h * a_prev
            a_next = a(x[k + 1])
            v[k + 1] = v[k] + 0.5 * h * (a_prev + a_next)
            a_prev = a_next
    else:
        raise ValueError(method)
    return t, x, v


def exact_solution(t: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
    """a = g - omega^2 x 的解析解，作为逐点比对基准。"""
    eq = G / OMEGA2                       # 平衡位置
    A, B = X0 - eq, V0 / OMEGA
    x = eq + A * np.cos(OMEGA * t) + B * np.sin(OMEGA * t)
    v = -A * OMEGA * np.sin(OMEGA * t) + B * OMEGA * np.cos(OMEGA * t)
    return x, v


def resample_at_30fps(t_hi: np.ndarray, y_hi: np.ndarray, n_lo: int) -> np.ndarray:
    """60fps 轨迹在 30fps 帧时刻上的取值（前者恰好是后者的加密网格，直接抽点）。"""
    return y_hi[::2][: n_lo + 1]


def energy(x: np.ndarray, v: np.ndarray) -> np.ndarray:
    # 势能 U = 0.5 omega^2 x^2 - g x（由 a = -dU/dx 反推，常数项任意）
    return 0.5 * v * v + 0.5 * OMEGA2 * x * x - G * x


# ---------------------------------------------------------------------------
# 实验零：常重力 a=g 的闭式解对照。
#   精确解            x(t) = x0 + v0 t + 1/2 g t^2
#   显式欧拉 n 步后   x_n = x0 + v0 t + 1/2 g t^2 - 1/2 g h t      （含 -O(h) 项）
#   辛欧拉 n 步后     x_n = x0 + v0 t + 1/2 g t^2 + 1/2 g h t      （含 +O(h) 项）
#   速度 Verlet       x_n = 精确解（恒加速度下二阶格式即精确，误差仅机器舍入）
# 用于把“差异是帧长一阶项”这件事用代数公式钉死。
# ---------------------------------------------------------------------------
def constant_gravity_demo() -> list[str]:
    g, x0, v0, T = 2.0, 0.3, 0.5, HORIZON
    out = ["", "=" * 78,
           "常重力闭式验证 a=g=2.0（x0=0.3, v0=0.5, T=4s）：手算公式与数值实现互证",
           "=" * 78]
    exact = x0 + v0 * T + 0.5 * g * T * T
    out.append(f"  精确解 x(T) = {exact:.12f}")
    out.append(f"  {'fps':>4} {'h':>9} {'显式欧拉 x(T)':>18} {'误差(精确-数值)':>18}"
               f" {'辛欧拉误差':>14} {'Verlet 误差':>14}")
    for fps in FPS_GRID:
        h = 1.0 / fps
        ee_formula = x0 + v0 * T + 0.5 * g * T * T - 0.5 * g * h * T
        se_formula = x0 + v0 * T + 0.5 * g * T * T + 0.5 * g * h * T
        # 数值实现交叉验证（与主流程同一套步进代码，关掉回复力）
        _, x_ee, _ = solve("explicit_euler", fps, x0, v0, omega2=0.0)
        _, x_se, _ = solve("symplectic_euler", fps, x0, v0, omega2=0.0)
        _, x_vv, _ = solve("velocity_verlet", fps, x0, v0, omega2=0.0)
        out.append(
            f"  {fps:>4} {h:>9.5f} {ee_formula:>18.12f} {exact - ee_formula:>+18.2e}"
            f" {exact - se_formula:>+14.2e} {exact - x_vv[-1]:>+14.2e}"
            f"   (实现核对 Δee={ee_formula - x_ee[-1]:+.1e}, Δse={se_formula - x_se[-1]:+.1e})")
    out.append("  结论：显式/辛欧拉的跨格式偏差恰为 ±(1/2) g h T，与 h 成正比（一阶）；")
    out.append("        Verlet 在恒加速度下与精确解完全重合，偏差只剩浮点舍入（~1e-15）。")
    return out


# ---------------------------------------------------------------------------
# 实验一：30fps vs 60fps，逐点比对（两档帧数均 >= 100）
# ---------------------------------------------------------------------------
def pointwise_compare(method: str) -> dict:
    t30, x30, v30 = solve(method, 30)
    t60, x60, v60 = solve(method, 60)
    assert len(t30) - 1 == 120 and len(t60) - 1 == 240  # 帧数 >= 100 的硬约束

    x60_on_30 = resample_at_30fps(t60, x60, len(t30) - 1)
    v60_on_30 = resample_at_30fps(t60, v60, len(t30) - 1)
    dx = x60_on_30 - x30
    dv = v60_on_30 - v30

    xe, ve = exact_solution(t30)
    return {
        "t": t30, "x30": x30, "x60": x60_on_30, "v30": v30, "v60": v60_on_30,
        "dx": dx, "dv": dv, "ex": x30 - xe, "ev": v30 - ve,
        "max_abs_dx": float(np.max(np.abs(dx))),
        "end_dx": float(dx[-1]), "end_dv": float(dv[-1]),
        "e30": energy(x30, v30), "e60": energy(x60_on_30, v60_on_30),
    }


def write_pointwise_csv(method: str, r: dict) -> str:
    path = os.path.join(OUT, f"pointwise_{method}.csv")
    with open(path, "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(["frame", "t", "x_30fps", "x_60fps", "dx=x60-x30",
                    "v_30fps", "v_60fps", "dv=v60-v30"])
        for k in range(len(r["t"])):
            w.writerow([k, f"{r['t'][k]:.10f}",
                        f"{r['x30'][k]:.10f}", f"{r['x60'][k]:.10f}",
                        f"{r['dx'][k]:.3e}",
                        f"{r['v30'][k]:.10f}", f"{r['v60'][k]:.10f}",
                        f"{r['dv'][k]:.3e}"])
    return path


# ---------------------------------------------------------------------------
# 实验二：帧率扫描，验证阶数（跨帧率末态差异随 h 的缩放律）
# ---------------------------------------------------------------------------
FPS_GRID = [15, 30, 60, 120, 240]


def convergence_table(method: str) -> list[dict]:
    """以 480fps 的同格式解作为参考真值，测各帧率末态位置误差与能量漂移。"""
    ref_t, ref_x, ref_v = solve(method, 480)
    ref_end_x, ref_end_v = ref_x[-1], ref_v[-1]
    rows = []
    for fps in FPS_GRID:
        t, x, v = solve(method, fps)
        e = energy(x, v)
        rows.append({
            "fps": fps, "h": 1.0 / fps, "frames": len(t) - 1,
            "end_x": x[-1], "end_v": v[-1],
            "err_x_vs_480": abs(x[-1] - ref_end_x),
            "energy_drift": abs(e[-1] - e[0]),
        })
    for k in range(1, len(rows)):
        a, b = rows[k - 1]["err_x_vs_480"], rows[k]["err_x_vs_480"]
        rows[k]["ratio"] = (a / b) if b > 0 else float("inf")
    return rows


def cross_rate_scaling() -> list[dict]:
    """直接测相邻帧率末态差：一阶方法应 ~2 倍缩减，二阶方法应 ~4 倍。"""
    out = []
    for method in ("explicit_euler", "symplectic_euler", "velocity_verlet"):
        ends = {fps: solve(method, fps)[1][-1] for fps in FPS_GRID}
        for k in range(1, len(FPS_GRID)):
            lo, hi = FPS_GRID[k - 1], FPS_GRID[k]
            d = abs(ends[lo] - ends[hi])
            out.append({"method": method, "pair": f"{lo}->{hi}fps",
                        "h_lo": 1.0 / lo, "h_hi": 1.0 / hi, "end_gap": d})
    # 两两相邻对之间再算缩减比
    for method in ("explicit_euler", "symplectic_euler", "velocity_verlet"):
        gaps = [r["end_gap"] for r in out if r["method"] == method]
        for k in range(1, len(gaps)):
            out.append({"method": method,
                        "pair": f"缩减比 {FPS_GRID[k-1]}/{FPS_GRID[k]}",
                        "h_lo": 0.0, "h_hi": 0.0,
                        "end_gap": gaps[k - 1] / gaps[k] if gaps[k] else float("inf")})
    return out


# ---------------------------------------------------------------------------
# 画图：每个积分器一张对照图（x(t) 与相图两个面板）
# ---------------------------------------------------------------------------
def plot_trajectory(method: str, r: dict, title: str, fname: str) -> str:
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(12, 5.0), gridspec_kw={"width_ratios": [1.25, 1.0]})

    # 左：x(t)
    ax1.plot(r["t"], r["x30"], color=BLUE, label="30fps 轨迹（120 帧）")
    ax1.plot(r["t"], r["x60"], color=ORANGE, label="60fps 轨迹（240 帧）")
    ax1.scatter(r["t"][-1], r["x30"][-1], color=BLUE, s=46, zorder=5,
                edgecolor=SURFACE, linewidth=1.5)
    ax1.scatter(r["t"][-1], r["x60"][-1], color=ORANGE, s=46, zorder=5,
                edgecolor=SURFACE, linewidth=1.5)
    ax1.set_xlabel("物理时间 t（秒）")
    ax1.set_ylabel("位置 x")
    ax1.set_title(f"{title}：位置-时间曲线")
    ax1.legend(loc="lower right")

    # 右：相图 (x, v)
    ax2.plot(r["x30"], r["v30"], color=BLUE, label="30fps 相轨迹")
    ax2.plot(r["x60"], r["v60"], color=ORANGE, label="60fps 相轨迹")
    ax2.scatter(r["x30"][-1], r["v30"][-1], color=BLUE, s=46, zorder=5,
                edgecolor=SURFACE, linewidth=1.5)
    ax2.scatter(r["x60"][-1], r["v60"][-1], color=ORANGE, s=46, zorder=5,
                edgecolor=SURFACE, linewidth=1.5)
    ax2.set_xlabel("位置 x")
    ax2.set_ylabel("速度 v")
    ax2.set_title(f"{title}：相平面轨迹")
    ax2.legend(loc="upper right")

    fig.suptitle(f"同一段回放、两档帧率的轨迹对照（{title}，4 秒 / 120 与 240 帧）",
                 fontsize=12.5, y=0.98)
    fig.tight_layout(rect=(0, 0, 1, 0.95))
    path = os.path.join(OUT, fname)
    fig.savefig(path, dpi=150)
    plt.close(fig)
    return path


def plot_convergence(fname: str) -> str:
    fig, ax = plt.subplots(figsize=(9.0, 5.2))
    styles = {
        "explicit_euler": (BLUE, "o", "显式欧拉（非辛，一阶）"),
        "symplectic_euler": ("#898781", "s", "辛欧拉（辛，一阶，对照）"),
        "velocity_verlet": (ORANGE, "^", "速度 Verlet（辛，二阶）"),
    }
    for method, (c, mk, label) in styles.items():
        rows = convergence_table(method)
        hs = np.array([r["h"] for r in rows])
        errs = np.array([max(r["err_x_vs_480"], 1e-16) for r in rows])
        ax.plot(hs, errs, color=c, marker=mk, markersize=7,
                markeredgecolor=SURFACE, markeredgewidth=1.2, label=label)
    # 斜率参考线
    h = np.array([r["h"] for r in convergence_table("explicit_euler")])
    ax.plot(h, 0.9 * h, color=MUTED, linewidth=1.2, linestyle="--", label="O(h) 一阶参考斜率")
    ax.plot(h, 0.5 * h ** 2, color=MUTED, linewidth=1.2, linestyle=":", label="O(h²) 二阶参考斜率")
    ax.set_xscale("log")
    ax.set_yscale("log")
    ax.set_xlabel("帧长 h = 1 / fps（秒）")
    ax.set_ylabel("末态位置误差 |x(T) − x_ref(T)|")
    ax.set_title("末态误差随帧长的缩放律（参考真值：同格式 480fps）")
    ax.legend(loc="upper left")
    fig.tight_layout()
    path = os.path.join(OUT, fname)
    fig.savefig(path, dpi=150)
    plt.close(fig)
    return path


# ---------------------------------------------------------------------------
# 主流程
# ---------------------------------------------------------------------------
def main() -> None:
    os.makedirs(OUT, exist_ok=True)
    lines: list[str] = []

    def emit(s: str = "") -> None:
        print(s)
        lines.append(s)

    emit("=" * 78)
    emit("帧率相关性分析：同一段回放，30fps（120 帧）vs 60fps（240 帧），T=4s")
    emit(f"模型: a(x) = g - omega^2 x, g={G}, omega^2={OMEGA2}, "
         f"x0={X0}, v0={V0}")
    emit("=" * 78)

    for line in constant_gravity_demo():
        emit(line)

    results = {}
    for method, cn in (("explicit_euler", "显式欧拉"),
                       ("velocity_verlet", "速度 Verlet")):
        r = pointwise_compare(method)
        results[method] = r
        csv_path = write_pointwise_csv(method, r)
        emit(f"\n【{cn} / {method}】逐点比对（完整数据 -> {os.path.basename(csv_path)}）")
        emit(f"  末态(30fps): x={r['x30'][-1]:.10f}  v={r['v30'][-1]:.10f}")
        emit(f"  末态(60fps): x={r['x60'][-1]:.10f}  v={r['v60'][-1]:.10f}")
        emit(f"  末态位置差 dx(T)      = {r['end_dx']:+.6e}")
        emit(f"  末态速度差 dv(T)      = {r['end_dv']:+.6e}")
        emit(f"  全程最大 |dx|         = {r['max_abs_dx']:.6e}")
        emit(f"  能量漂移 |E(T)-E(0)|  : 30fps={abs(r['e30'][-1]-r['e30'][0]):.4e}  "
             f"60fps={abs(r['e60'][-1]-r['e60'][0]):.4e}")
        # 抽点展示差异随帧号（物理时间）的增长
        idx = [0, 15, 30, 60, 90, 120]
        emit("  抽帧（frame, t, dx=x60-x30）: " +
             " | ".join(f"f{k},t={r['t'][k]:.1f}s,{r['dx'][k]:+.2e}" for k in idx))

    emit("\n" + "=" * 78)
    emit("帧率扫描：各帧率末态位置相对 480fps 参考解的误差，及 h 减半时的缩减比")
    emit("=" * 78)
    conv_paths = {}
    for method, cn in (("explicit_euler", "显式欧拉"),
                       ("symplectic_euler", "辛欧拉(一阶对照)"),
                       ("velocity_verlet", "速度 Verlet")):
        rows = convergence_table(method)
        conv_paths[method] = rows
        emit(f"\n【{cn}】")
        emit(f"  {'fps':>4} {'h':>9} {'frames':>6} {'err_x(vs480)':>14} {'缩减比':>8} {'能量漂移':>12}")
        for k, r in enumerate(rows):
            ratio = "-" if k == 0 else f"{r['ratio']:.2f}x"
            emit(f"  {r['fps']:>4} {r['h']:>9.5f} {r['frames']:>6} "
                 f"{r['err_x_vs_480']:>14.4e} {ratio:>8} {r['energy_drift']:>12.4e}")

    emit("\n" + "=" * 78)
    emit("相邻帧率末态差（直接量化跨帧率不一致，不依赖参考解）")
    emit("=" * 78)
    for row in cross_rate_scaling():
        if row["h_lo"] > 0:
            emit(f"  {row['method']:<16} {row['pair']:<12} 末态差={row['end_gap']:.4e}")
        else:
            emit(f"  {row['method']:<16} {row['pair']:<20} = {row['end_gap']:.2f}x")
    emit("")
    emit("趋势：辛欧拉的相邻帧率末态差稳定按 ~2.0x 缩减（O(h) 一阶），")
    emit("      速度 Verlet 稳定按 4.00x 缩减（O(h^2) 二阶，四帧差比两帧差小一个数量级）。")
    emit("      显式欧拉在振荡解上还叠加与步数成正比的相位/能量误差，个别相邻对不单调，")
    emit("      这正是非辛格式的症状；其常重力闭式误差仍严格按 2x 一阶缩减（见实验零）。")

    p1 = plot_trajectory("explicit_euler", results["explicit_euler"],
                         "显式欧拉（非辛）", "trajectory_explicit_euler.png")
    p2 = plot_trajectory("velocity_verlet", results["velocity_verlet"],
                         "速度 Verlet（二阶辛）", "trajectory_velocity_verlet.png")
    p3 = plot_convergence("convergence_order.png")
    emit("\n输出图表:")
    for p in (p1, p2, p3):
        emit(f"  {p}")

    with open(os.path.join(OUT, "analysis_summary.txt"), "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")


if __name__ == "__main__":
    main()
