#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""独立复核 artifacts/bias：

1) 每行整数恒等式  devNum = fixedSum*S - refNum，末态偏差 = Σ epsNum/S；
2) “偏置 = 每帧误差 × 帧数”  N*meanEps == endDev（精确，容差仅来自 JSON 浮点）；
3) half_asym 的 +1/2 每两块，精确给出 +250；
4) 有界断言：残差 |dev|<=1，收缩 |dev|<=15；
5) 用 Python 独立实现 FNV-1a 混合，逐行比对 frame_hashes.txt 的全部 1000 个哈希。

退出码 0 表示全部通过。
"""
import csv
import json
import os
import sys
from collections import defaultdict

OUT = sys.argv[1] if len(sys.argv) > 1 else "artifacts/bias"
S = 1 << 16
MASK = (1 << 64) - 1
OFF = 14695981039346656037
PRIME = 1099511628211

failures = []
checks = 0


def ok(cond, msg):
    global checks
    checks += 1
    if not cond:
        failures.append(msg)
        print("FAIL:", msg)
    else:
        print("ok  :", msg)


def fnv_values(tick, raws):
    h = OFF
    for v in [tick & 0xFFFFFFFF] + [x & 0xFFFFFFFF for x in raws]:
        h ^= v
        h = (h * PRIME) & MASK
    return h


# ---------- 读取 MAC ----------
rows = defaultdict(list)
with open(os.path.join(OUT, "bias_frames.csv"), newline="") as fh:
    for r in csv.DictReader(fh):
        rows[(r["case"], r["mode"])].append(r)

for key, rs in rows.items():
    eps_sum = 0
    for r in rs:
        fs, rn, dn = int(r["fixedSum"]), int(r["refNum"]), int(r["devNum"])
        eps = int(r["epsNum"])
        eps_sum += eps
        if fs * S - rn != dn:
            ok(False, f"{key} tick={r['tick']} devNum 恒等式")
            break
    else:
        ok(True, f"{key} 全部 {len(rs)} 行 devNum=fixedSum*S-refNum 成立")
    end = int(rs[-1]["devNum"])
    ok(end == eps_sum, f"{key} 末态 devNum == Σ epsNum ({end} == {eps_sum})")

# ---------- N*meanEps 与 summary 一致 ----------
summary = json.load(open(os.path.join(OUT, "summary.json")))
for st in summary["macCurves"]:
    key = (st["case"], st["mode"])
    end_ulp = int(rows[key][-1]["devNum"]) / S
    ok(abs(end_ulp - st["endDevUlp"]) < 1e-9, f"{key} summary 末态 {end_ulp:.2f}")
    ok(abs(st["nTimesMeanEps"] - st["endDevUlp"]) < 1e-9,
       f"{key} N*meanEps={st['nTimesMeanEps']:.3f} == end={st['endDevUlp']:.3f}")

# ---------- half_asym 精确 +1/2 每块 ----------
ha = rows[("half_asym", "tz")]
block_ok = True
for k in range(500):
    e0 = int(ha[2 * k]["epsNum"])
    e1 = int(ha[2 * k + 1]["epsNum"])
    if (e0, e1) != (-S // 4, 3 * S // 4) or e0 + e1 != S // 2:
        block_ok = False
        break
ok(block_ok, "half_asym 每块 epsNum=(-S/4,+3S/4)，块偏置 +S/2 = +0.5 ULP")
ok(int(ha[-1]["devNum"]) == 250 * S, "half_asym TZ 1000 帧精确 +250 ULP")

# ---------- 有界断言 ----------
bounded = defaultdict(list)
with open(os.path.join(OUT, "bounded_frames.csv"), newline="") as fh:
    for r in csv.DictReader(fh):
        bounded[r["series"]].append(float(r["devUlp"]))
ok(all(-1.0 < d <= 0.0 for d in bounded["residue_floor"]),
   f"残差累加全程 -1<D<=0（实测 [{min(bounded['residue_floor']):.6f}, {max(bounded['residue_floor']):.6f}]）")
ok(all(abs(d) <= 15.0 for d in bounded["contract_floor"]), "contract_floor 全程 |D|<=15")
ok(all(abs(d) <= 15.0 for d in bounded["contract_tz"]), "contract_tz 全程 |D|<=15")

# ---------- 独立重算并比对全部哈希 ----------
canon = rows[("single_pos", "tz")]
hash_lines = open(os.path.join(OUT, "frame_hashes.txt")).read().strip().splitlines()
ok(len(hash_lines) == 1000, f"哈希文件恰好 1000 行（{len(hash_lines)}）")
ok(len(set(hash_lines)) == 1000, "1000 个哈希互不相同")
mismatch = 0
ticks_seen = []
for line, r in zip(hash_lines, canon):
    left, hx = line.split()
    tick = int(left.split("=")[1])
    ticks_seen.append(tick)
    want = f"{fnv_values(tick, [int(r['fixedSum']), int(r['rounded'])]):016x}"
    if hx.split("=")[1] != want:
        mismatch += 1
ok(ticks_seen == list(range(1000)), "哈希 tick 严格为 0..999")
ok(mismatch == 0, f"Python 独立 FNV 重算全部 1000 个哈希一致（不一致 {mismatch}）")

# 全 artifacts 中只有 frame_hashes.txt 含真正的逐帧 hash=<16hex> 行
import re
pat = re.compile(r"hash=[0-9a-f]{16}")
hash_files = []
for root, _, files in os.walk("artifacts"):
    for name in files:
        p = os.path.join(root, name)
        try:
            if pat.search(open(p, errors="ignore").read()):
                hash_files.append(p)
        except OSError:
            pass
ok(hash_files == ["artifacts/bias/frame_hashes.txt"],
   f"全 artifacts 仅一份逐帧哈希数据：{hash_files}")

print(f"\n{checks} 项检查，{checks - len(failures)} 通过，{len(failures)} 失败")
sys.exit(1 if failures else 0)
