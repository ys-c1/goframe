#!/usr/bin/env python3
"""独立核对 gravity 定点量化实验产物。

只使用标准库，从 artifacts/projectile_metrics.json 读数并验证：
  1. 恰好两条轨迹 × 三档位数 = 6 行，且含横向偏移列；
  2. 分子 / 分子位数 / δg / 二次系数为题目推导的精确值；
  3. 同帧落点差严格满足二次定律与逐档比例 256、65536；
  4. 首次越界帧 16 / 256 / 65536，且边界两帧把容差夹在中间；
  5. 落地判定与“落地帧 < 越界帧”一致（容差之内才算过）；
  6. 横向偏移非零、为负（g 向上舍入）且随位数升高单调缩小。

用法：python3 -I analysis/verify_gravity.py [metrics.json]
"""
from __future__ import annotations

import json
import sys
from fractions import Fraction
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_JSON = ROOT / "artifacts" / "projectile_metrics.json"

EXPECTED = {
    8:  {"num": 2509,         "digits": 4,  "dg": Fraction(1, 1280),        "k": 16},
    16: {"num": 642253,       "digits": 6,  "dg": Fraction(1, 327680),      "k": 256},
    32: {"num": 42090679501,  "digits": 11, "dg": Fraction(1, 21474836480), "k": 65536},
}


def err_at(n: int, dg: Fraction) -> Fraction:
    return abs(dg) * n * (n + 1) / 2


def main() -> int:
    path = Path(sys.argv[1]) if len(sys.argv) > 1 else DEFAULT_JSON
    data = json.loads(path.read_text(encoding="utf-8"))
    eps = Fraction(data["tolerance"])
    rows = data["rows"]
    failures: list[str] = []

    def check(cond: bool, msg: str) -> None:
        print(("PASS " if cond else "FAIL ") + msg)
        if not cond:
            failures.append(msg)

    traj_ids = sorted({r["traj_id"] for r in rows})
    check(traj_ids == ["A", "B"], f"两条抛物轨迹 A/B 各一份（实际 {traj_ids}）")
    check(len(rows) == 6, f"共 6 行 = 2 轨迹 × 3 档（实际 {len(rows)}）")
    check(all("horizontal_offset_dx" in r for r in rows), "每行都有横向偏移列")

    # 常量、系数、越界帧（与轨迹无关，逐档核对一次）。
    for bits, e in EXPECTED.items():
        tier_rows = [r for r in rows if r["frac_bits"] == bits]
        r0 = tier_rows[0]
        check(r0["numerator"] == e["num"], f"f={bits} 分子={e['num']}")
        check(r0["num_digits"] == e["digits"], f"f={bits} 分子位数={e['digits']}")
        check(Fraction(r0["dgravity_rat"]) == e["dg"], f"f={bits} δg={e['dg']}")
        check(r0["first_breach_frame"] == e["k"], f"f={bits} 首次越界帧={e['k']}")
        km1, k = e["k"] - 1, e["k"]
        check(err_at(km1, e["dg"]) <= eps < err_at(k, e["dg"]),
              f"f={bits} 边界核对 Δ({km1})≤ε<Δ({k})")

    # 每条轨迹逐档比对落点差、判定与横向偏移。
    for tid in traj_ids:
        rs = sorted((r for r in rows if r["traj_id"] == tid),
                    key=lambda r: r["frac_bits"])
        e8 = Fraction(rs[0]["land_diff_y_rat"])
        e16 = Fraction(rs[1]["land_diff_y_rat"])
        e32 = Fraction(rs[2]["land_diff_y_rat"])
        nstar = rs[0]["ref_land_frame"]
        check(e8 == 256 * e16, f"轨迹 {tid}: Δ_Q8 = 256·Δ_Q16（n*={nstar}）")
        check(e16 == 65536 * e32, f"轨迹 {tid}: Δ_Q16 = 65536·Δ_Q32")
        for r in rs:
            diff = Fraction(r["land_diff_y_rat"])
            check(diff == err_at(nstar, EXPECTED[r["frac_bits"]]["dg"]),
                  f"轨迹 {tid}/{r['tier']}: 落点差=δg·n(n+1)/2")
            check(r["pass_at_landing"] == (nstar < r["first_breach_frame"]),
                  f"轨迹 {tid}/{r['tier']}: 判定与 n*<k 一致")
            check((diff <= eps) == r["pass_at_landing"],
                  f"轨迹 {tid}/{r['tier']}: 容差核对一致")
            dx = r["horizontal_offset_dx"]
            check(dx < 0, f"轨迹 {tid}/{r['tier']}: 横向射程差为负（{dx:.3g}）")
            check(r["same_frame_dx"] == 0,
                  f"轨迹 {tid}/{r['tier']}: 同帧横向偏移严格为 0（重力只在竖直轴）")
            check(abs(r["landing_gap"] - abs(dx)) < 1e-12,
                  f"轨迹 {tid}/{r['tier']}: 地面落点距=|横向射程差|")
            check((Fraction(str(r["landing_gap"])) <= eps) == r["landing_gap_within_tolerance"],
                  f"轨迹 {tid}/{r['tier']}: 地面落点距容差判定一致")
        dxs = [abs(r["horizontal_offset_dx"]) for r in rs]
        check(dxs[0] > dxs[1] > dxs[2],
              f"轨迹 {tid}: 横向偏移随位数升高单调缩小")

    if failures:
        print(f"\n{len(failures)} 项核对失败")
        return 1
    print("\n全部核对通过：三档落点差逐档成比例，越界帧与容差一致，两条轨迹均含横向偏移列。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
