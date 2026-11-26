#!/usr/bin/env python3
"""Independent cross-check of restored snapshots against the full baseline.

Run with: python3 -I scripts/verify_snapshots.py artifacts/snapshots
Reads full.jsonl + restored_b*.jsonl from the given directory and recomputes
max errors, worst tick/body and first-divergence tick without any Go code.
"""
import json
import os
import sys

d = sys.argv[1]
POS_LIMIT, VEL_LIMIT = 65536, 2048


def load(name):
    out = []
    with open(os.path.join(d, name), encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                out.append(json.loads(line))
    return out


full = load("full.jsonl")
bodies = len(full[0]["raw"]) // 4
for bits in (21, 24, 28):
    rest = load(f"restored_b{bits}.jsonl")
    assert len(rest) == len(full)
    pos = (0, None)
    vel = (0, None)
    first_pos = first_vel = None
    for fr, fb in zip(rest, full):
        assert fr["tick"] == fb["tick"]
        t = fb["tick"]
        for i in range(bodies):
            for kind, k in (("pos", 0), ("pos", 1), ("vel", 2), ("vel", 3)):
                e = abs(fb["raw"][4 * i + k] - fr["raw"][4 * i + k])
                if kind == "pos":
                    if e and first_pos is None:
                        first_pos = t
                    if e > pos[0]:
                        pos = (e, (t, i, k))
                else:
                    if e and first_vel is None:
                        first_vel = t
                    if e > vel[0]:
                        vel = (e, (t, i, k))
    ok = pos[0] <= POS_LIMIT and vel[0] <= VEL_LIMIT
    print(
        f"b{bits}: pos_max={pos[0]}/65536 at(tick,body,ch)={pos[1]} "
        f"vel_max={vel[0]}/2048 at={vel[1]} "
        f"first_pos_tick={first_pos} first_vel_tick={first_vel} "
        f"verdict={'PASS' if ok else 'FAIL'}"
    )
