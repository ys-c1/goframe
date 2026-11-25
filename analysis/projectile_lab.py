#!/usr/bin/env python3
"""斜抛积分误差实验：生成对照表、JSON 指标和两张轨迹图。

输入由 `go run ./cmd/projectile` 生成：
artifacts/projectile_q16_traces.csv

本脚本只使用标准库与 matplotlib/numpy；物理参考解为恒定重力下的解析解。
"""
from __future__ import annotations

import csv
import json
import math
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np
from matplotlib import font_manager

ROOT = Path(__file__).resolve().parents[1]
ART = ROOT / "artifacts"
TRACE_CSV = ART / "projectile_q16_traces.csv"

Q = 65536.0
H = 1.0
G = 10.0
DT = 1.0 / 32.0
ANGLE_LABELS = {
    "horizontal": "水平 α=0°",
    "forty_five": "45°",
    "vertical": "竖直 α=90°",
}
ORDER = ["horizontal", "forty_five", "vertical"]

# 数据可视化规范色板（light mode 前三个分类色通过校验）。
BLUE = "#2a78d6"
ORANGE = "#eb6834"
AQUA = "#1baf7a"
INK = "#0b0b0b"
SECONDARY = "#52514e"
GRID = "#e1e0d9"
RED = "#d03b3b"

for font_path in [
    Path.home() / ".local/share/fonts/NotoSansCJKsc-Regular.ttf",
    "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
    "/usr/share/fonts/opentype/noto/NotoSansCJKsc-Regular.otf",
]:
    p = Path(font_path)
    if p.exists():
        font_manager.fontManager.addfont(str(p))
plt.rcParams["font.family"] = ["Noto Sans CJK SC", "DejaVu Sans"]
plt.rcParams["axes.unicode_minus"] = False


def read_traces() -> dict[str, dict[str, list[dict[str, float]]]]:
    traces: dict[str, dict[str, list[dict[str, float]]]] = {
        a: {"semi_implicit": [], "exact_component": []} for a in ORDER
    }
    with TRACE_CSV.open(newline="", encoding="utf-8") as f:
        for row in csv.DictReader(f):
            angle = row["angle"]
            scheme = row["scheme"]
            traces[angle][scheme].append(
                {
                    "step": int(row["step"]),
                    "x": int(row["x_raw"]) / Q,
                    "y": int(row["y_raw"]) / Q,
                    "vx": int(row["vx_raw"]) / Q,
                    "vy": int(row["vy_raw"]) / Q,
                }
            )
    return traces


def landing_time(vy0: float, h0: float = H, g: float = G) -> float:
    return (vy0 + math.sqrt(vy0 * vy0 + 2.0 * g * h0)) / g


def ref_position(t: float, vx0: float, vy0: float) -> tuple[float, float]:
    return vx0 * t, H + vy0 * t - 0.5 * G * t * t


def polar_coupled_path(vx0: float, vy0: float, h: float = DT):
    """题述“按速度方向/夹角耦合”的一阶极坐标积分基线。

    ds/dt = g·u = -g sin β，dβ/dt = -g cos β / s。
    它的切向/法向误差分配显式依赖速度与重力方向的夹角。
    """
    x, y = 0.0, H
    vx, vy = vx0, vy0
    t = 0.0
    path = [(t, x, y)]
    while t < 4.0:
        px, py, pt = x, y, t
        speed = math.hypot(vx, vy)
        beta = math.atan2(vy, vx)
        speed_new = speed - G * math.sin(beta) * h
        beta_new = beta - G * math.cos(beta) / max(speed, 1e-12) * h
        vx = speed_new * math.cos(beta_new)
        vy = speed_new * math.sin(beta_new)
        x += vx * h
        y += vy * h
        t += h
        path.append((t, x, y))
        if y <= 0.0:
            frac = py / (py - y)
            land_t = pt + frac * h
            land_x = px + frac * (x - px)
            return path, land_t, land_x
    raise RuntimeError("coupled path did not land")


def crossing_from_xy(xs: list[float], ys: list[float], h: float):
    for i in range(1, len(ys)):
        if ys[i] <= 0.0:
            frac = ys[i - 1] / (ys[i - 1] - ys[i])
            return (i - 1 + frac) * h, xs[i - 1] + frac * (xs[i] - xs[i - 1])
    return None, None


def main() -> None:
    ART.mkdir(parents=True, exist_ok=True)
    traces = read_traces()
    rows = []
    compare = []
    plot_data = {}

    for angle in ORDER:
        semi = traces[angle]["semi_implicit"]
        exact = traces[angle]["exact_component"]
        vx0 = semi[0]["vx"]
        vy0 = semi[0]["vy"]
        t_star = landing_time(vy0)
        n = int(round(t_star / DT))
        t_check = n * DT

        x_ref, y_ref = ref_position(t_check, vx0, vy0)
        s = semi[n]
        dx = s["x"] - x_ref
        dy = s["y"] - y_ref
        ratio = abs(dx / dy) if abs(dy) > 1e-15 else 0.0

        e = exact[n]
        dx_exact_q16 = e["x"] - x_ref
        dy_exact_q16 = e["y"] - y_ref

        # 高精度夹角耦合基线与高精度分量精确格式。
        coupled_path, coupled_t, coupled_xland = polar_coupled_path(vx0, vy0)
        cxs = [p[1] for p in coupled_path]
        cys = [p[2] for p in coupled_path]
        c_at_n = coupled_path[min(n, len(coupled_path) - 1)]
        cx_ref_n, cy_ref_n = ref_position(c_at_n[0], vx0, vy0)
        c_dx = c_at_n[1] - cx_ref_n
        c_dy = c_at_n[2] - cy_ref_n
        c_norm = math.hypot(c_dx, c_dy)

        # 精确分量格式在浮点下与解析解相同；这里用同一公式逐步推进，
        # 并做帧间线性插值来找落点。
        ex, ey, evx, evy = 0.0, H, vx0, vy0
        exs, eys = [ex], [ey]
        for _ in range(n + 5):
            ex += evx * DT
            ey += evy * DT - 0.5 * G * DT * DT
            evy -= G * DT
            exs.append(ex)
            eys.append(ey)
        exact_t, exact_xland = crossing_from_xy(exs, eys, DT)

        range_ref = vx0 * t_star
        coupled_land_dx = coupled_xland - range_ref
        exact_land_dx = exact_xland - range_ref
        coupled_dt = coupled_t - t_star
        exact_dt = exact_t - t_star

        exact_cp_norm = math.hypot(dx_exact_q16, dy_exact_q16)
        # 图中展示实际 Q16 精确分量轨迹；其 checkpoint 残差是定点舍入级。
        q16_cp_norm = exact_cp_norm
        q16_lsb = 1.0 / Q
        conservative_residual = max(q16_cp_norm, q16_lsb)

        rows.append(
            {
                "angle": angle,
                "angle_label": ANGLE_LABELS[angle],
                "theta_deg": {"horizontal": 0, "forty_five": 45, "vertical": 90}[angle],
                "h_s": DT,
                "N": n,
                "t_check_s": t_check,
                "dx_m": dx,
                "dy_m": dy,
                "abs_dx_over_dy": ratio,
                "expected_gx_over_gy": 0.0,
                "gx_m_s2": 0.0,
                "gy_m_s2": -G,
                "dx_exact_q16_m": dx_exact_q16,
                "dy_exact_q16_m": dy_exact_q16,
            }
        )
        compare.append(
            {
                "angle": angle,
                "N": n,
                "t_star_s": t_star,
                "range_ref_m": range_ref,
                "coupled_dx_cp_m": c_dx,
                "coupled_dy_cp_m": c_dy,
                "coupled_cp_norm_m": c_norm,
                "component_q16_cp_norm_m": q16_cp_norm,
                "q16_lsb_m": q16_lsb,
                "cp_decrease_lower_bound": c_norm / conservative_residual,
                "coupled_landing_dx_m": coupled_land_dx,
                "exact_component_landing_dx_m": exact_land_dx,
                "coupled_landing_dt_s": coupled_dt,
                "exact_component_landing_dt_s": exact_dt,
                "landing_time_improvement_factor": abs(coupled_dt) / max(abs(exact_dt), 1e-12),
            }
        )
        plot_data[angle] = {
            "vx0": vx0,
            "vy0": vy0,
            "t_star": t_star,
            "coupled": coupled_path,
            "exact_q16": exact,
            "semi_q16": semi,
        }

    metrics = {"parameters": {"g": G, "H": H, "h": DT, "Q16": True}, "component_rows": rows, "comparison": compare}
    (ART / "projectile_metrics.json").write_text(json.dumps(metrics, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    with (ART / "projectile_component_rows.csv").open("w", newline="", encoding="utf-8") as f:
        fields = list(rows[0].keys())
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader()
        w.writerows(rows)

    with (ART / "projectile_landing_compare.csv").open("w", newline="", encoding="utf-8") as f:
        fields = list(compare[0].keys())
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader()
        w.writerows(compare)

    write_markdown_tables(rows, compare)
    write_frame_sweep()
    make_before_plot(plot_data)
    make_after_plot(plot_data)

    for r in rows:
        print(
            f"{r['angle']:12s} N={r['N']:3d} dx={r['dx_m']: .9f} m "
            f"dy={r['dy_m']: .9f} m |dx/dy|={r['abs_dx_over_dy']:.6f}"
        )


def write_frame_sweep() -> None:
    """验证一阶分量格式的落地误差随 h 近似线性减半。"""
    hs = [1 / 16, 1 / 32, 1 / 64]
    out = []
    for angle in ORDER:
        theta = {"horizontal": 0.0, "forty_five": math.pi / 4, "vertical": math.pi / 2}[angle]
        vx0, vy0 = 12.0 * math.cos(theta), 12.0 * math.sin(theta)
        t_star = landing_time(vy0)
        range_ref = vx0 * t_star
        for h in hs:
            x, y = 0.0, H
            vx, vy = vx0, vy0
            t = 0.0
            while True:
                px, py, pt = x, y, t
                vy -= G * h
                x += vx * h
                y += vy * h
                t += h
                if y <= 0:
                    frac = py / (py - y)
                    land_t = pt + frac * h
                    land_x = px + frac * (x - px)
                    break
            n = int(round(t_star / h))
            out.append(
                {
                    "angle": angle,
                    "h_s": h,
                    "N_checkpoint": n,
                    "same_time_dy_m": -0.5 * G * h * h * n,
                    "landing_dx_m": land_x - range_ref,
                    "landing_dt_s": land_t - t_star,
                }
            )

    with (ART / "projectile_frame_sweep.csv").open("w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=list(out[0].keys()))
        w.writeheader()
        w.writerows(out)

    lines = [
        "",
        "## 帧长扫描：一阶分量格式的误差随 h 近似线性下降",
        "",
        "| 夹角档 | h (s) | 同时刻 Δy 理论值 (m) | 落地 Δx (m) | 落地 Δt (s) |",
        "|---|---:|---:|---:|---:|",
    ]
    for r in out:
        lines.append(
            f"| {ANGLE_LABELS[r['angle']]} | 1/{round(1 / r['h_s'])} | "
            f"{r['same_time_dy_m']:+.6f} | {r['landing_dx_m']:+.6f} | {r['landing_dt_s']:+.6f} |"
        )
    with (ART / "projectile_tables.md").open("a", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")


def write_markdown_tables(rows, compare) -> None:
    lines = [
        "# 斜抛实验自动生成数据表",
        "",
        f"参数：g={G:g} m/s²，出手高度 H={H:g} m，帧长 h=1/32 s，Q16.16。",
        "",
        "## 分量一阶累积误差（在各自解析落地时刻附近的整数帧）",
        "",
        "| 夹角档 | N | t=N h (s) | 水平偏差 Δx (m) | 竖直偏差 Δy (m) | abs(Δx/Δy) | abs(gx/gy) |",
        "|---|---:|---:|---:|---:|---:|---:|",
    ]
    for r in rows:
        lines.append(
            f"| {r['angle_label']} | {r['N']} | {r['t_check_s']:.4f} | "
            f"{r['dx_m']:+.9f} | {r['dy_m']:+.9f} | {r['abs_dx_over_dy']:.6f} | 0 |"
        )
    lines += [
        "",
        "## 修复前夹角耦合基线 vs 修复后分量格式",
        "",
        "| 夹角档 | 耦合同时刻偏差范数 (m) | Q16分量残差范数 (m) | 保守下降倍数下界 | 耦合落地 Δx (m) | 分量落地 Δx (m) | 耦合 Δt (s) | 分量 Δt (s) |",
        "|---|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for c in compare:
        label = ANGLE_LABELS[c["angle"]]
        residual = c["component_q16_cp_norm_m"]
        residual_text = f"{residual:.9f}" if residual > 0 else f"0（<1 LSB={c['q16_lsb_m']:.2e}）"
        lines.append(
            f"| {label} | {c['coupled_cp_norm_m']:.9f} | {residual_text} | "
            f"≥{c['cp_decrease_lower_bound']:.0f}× | {c['coupled_landing_dx_m']:+.9f} | "
            f"{c['exact_component_landing_dx_m']:+.9f} | {c['coupled_landing_dt_s']:+.9f} | "
            f"{c['exact_component_landing_dt_s']:+.9f} |"
        )
    (ART / "projectile_tables.md").write_text("\n".join(lines) + "\n", encoding="utf-8")


def _dense_reference(d: dict) -> tuple[np.ndarray, np.ndarray]:
    ts = np.linspace(0.0, d["t_star"], 240)
    xs = d["vx0"] * ts
    ys = H + d["vy0"] * ts - 0.5 * G * ts * ts
    return xs, ys


def make_before_plot(plot_data) -> None:
    fig, axes = plt.subplots(1, 3, figsize=(15.2, 4.9), constrained_layout=True)
    fig.patch.set_facecolor("#fcfcfb")
    for ax, angle in zip(axes, ORDER):
        d = plot_data[angle]
        xs_ref, ys_ref = _dense_reference(d)
        coupled = d["coupled"]
        xs = [p[1] for p in coupled]
        ys = [p[2] for p in coupled]
        ax.plot(xs_ref, ys_ref, color=INK, lw=2.0, ls="--", alpha=0.72, label="解析抛物线")
        ax.plot(xs, ys, color=ORANGE, lw=2.4, marker="o", ms=3.2, markevery=4, label="修复前：夹角耦合一阶")
        ax.scatter([xs[-1]], [ys[-1]], s=58, color=RED, edgecolor="white", linewidth=1.6, zorder=5, label="越界帧")
        ax.axhline(0, color="#898781", lw=1.0)
        ax.set_title(ANGLE_LABELS[angle], fontsize=13, weight="bold")
        ax.set_xlabel("水平位置 x (m)")
        ax.grid(True, color=GRID, lw=0.8)
        ax.set_facecolor("#fcfcfb")
        for spine in ("top", "right"):
            ax.spines[spine].set_visible(False)
        if angle == "horizontal":
            ax.set_ylabel("竖直位置 y (m)")
            ax.legend(loc="upper right", frameon=False, fontsize=9)
        if angle == "vertical":
            ax.set_xlim(-0.45, 0.45)
    fig.suptitle("修复前：误差按发射方向/重力夹角耦合，竖直档累积最久、偏差最大", fontsize=15, weight="bold")
    fig.savefig(ART / "trajectory-before.png", dpi=180, facecolor=fig.get_facecolor())
    plt.close(fig)


def make_after_plot(plot_data) -> None:
    fig, axes = plt.subplots(1, 3, figsize=(15.2, 4.9), constrained_layout=True)
    fig.patch.set_facecolor("#fcfcfb")
    for ax, angle in zip(axes, ORDER):
        d = plot_data[angle]
        xs_ref, ys_ref = _dense_reference(d)
        exact = d["exact_q16"]
        xs = [exact[0]["x"]]
        ys = [exact[0]["y"]]
        for i in range(1, len(exact)):
            if exact[i]["y"] <= 0.0:
                p0, p1 = exact[i - 1], exact[i]
                frac = p0["y"] / (p0["y"] - p1["y"])
                xs.append(p0["x"] + frac * (p1["x"] - p0["x"]))
                ys.append(0.0)
                break
            xs.append(exact[i]["x"])
            ys.append(exact[i]["y"])
        ax.plot(xs_ref, ys_ref, color=INK, lw=2.0, ls="--", alpha=0.72, label="解析抛物线")
        ax.plot(xs, ys, color=BLUE, lw=2.4, marker="o", ms=3.0, markevery=8, label="修复后：Q16 分量精确积分")
        ax.axhline(0, color="#898781", lw=1.0)
        ax.set_title(ANGLE_LABELS[angle], fontsize=13, weight="bold")
        ax.set_xlabel("水平位置 x (m)")
        ax.grid(True, color=GRID, lw=0.8)
        ax.set_facecolor("#fcfcfb")
        for spine in ("top", "right"):
            ax.spines[spine].set_visible(False)
        if angle == "horizontal":
            ax.set_ylabel("竖直位置 y (m)")
            ax.legend(loc="upper right", frameon=False, fontsize=9)
        if angle == "vertical":
            ax.set_xlim(-0.45, 0.45)
    fig.suptitle("修复后：x/y 分量独立积分，轨迹与解析解重合，残差降至 Q16 舍入级", fontsize=15, weight="bold")
    fig.savefig(ART / "trajectory-after.png", dpi=180, facecolor=fig.get_facecolor())
    plt.close(fig)


if __name__ == "__main__":
    main()
