#!/usr/bin/env python3
"""Decode the exact per-tick command schedule from full.jsonl by velocity diffs.

Run with: python3 -I scripts/decode_scenario.py
Reads the baseline snapshot, prints impulses inferred from velX/velY changes.
"""
import json
import sys

PATH = sys.argv[1] if len(sys.argv) > 1 else "artifacts/snapshots/full.jsonl"

frames = []
with open(PATH, encoding="utf-8") as f:
    for line in f:
        line = line.strip()
        if line:
            frames.append(json.loads(line))

bodies = len(frames[0]["raw"]) // 4
prev_vel = [(0, 0) for _ in range(bodies)]
print(f"frames={len(frames)} bodies={bodies}")
for fr in frames:
    tick = fr["tick"]
    raw = fr["raw"]
    for i in range(bodies):
        vx, vy = raw[4 * i + 2], raw[4 * i + 3]
        dx, dy = vx - prev_vel[i][0], vy - prev_vel[i][1]
        if dx or dy:
            print(f"tick={tick:2d} body={i} push=({dx},{dy})")
        prev_vel[i] = (vx, vy)
