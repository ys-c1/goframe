#!/usr/bin/env bash
# 生成“全局随机源不可复现 -> 按实体编号派生子序列”的全部证据与指标。
# 所有序列都由全新启动的独立进程产出；产物落在 artifacts/perturb/。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ART="$ROOT/artifacts/perturb"
mkdir -p "$ART/process"

PB="$(mktemp /tmp/goframe-perturb.XXXXXX)"
GB="$(mktemp /tmp/goframe-goframe.XXXXXX)"
trap 'rm -f "$PB" "$GB"' EXIT

cd "$ROOT"
go build -o "$PB" ./cmd/perturb
go build -o "$GB" ./cmd/goframe

SEED=20261126
B=8
F=12

# ---------- 两版序列各存一份（同种子、同参数，便于逐项对照） ----------
"$PB" seq --version global  --seed "$SEED" --bodies "$B" --frames "$F" \
    > "$ART/global_sequence.txt"
"$PB" seq --version derived --seed "$SEED" --bodies "$B" --frames "$F" \
    > "$ART/derived_sequence.txt"

# ---------- 证据：两个独立 OS 进程 ----------
# 旧做法 + 不控制种子（Go 1.20+ 顶层全局源进程启动随机播种）
"$PB" seq --version global --seed -1 --bodies "$B" --frames "$F" \
    > "$ART/global.auto.proc1.txt"
"$PB" seq --version global --seed -1 --bodies "$B" --frames "$F" \
    > "$ART/global.auto.proc2.txt"

# 对照：全局源但种子与次序都冻结时，两个进程可以一致
"$PB" seq --version global --seed "$SEED" --order frame --bodies "$B" --frames "$F" \
    > "$ART/global.fixed.proc1.txt"
"$PB" seq --version global --seed "$SEED" --order frame --bodies "$B" --frames "$F" \
    > "$ART/global.fixed.proc2.txt"

# 新做法：派生源，两个进程必须逐项相同
"$PB" seq --version derived --seed "$SEED" --order frame --bodies "$B" --frames "$F" \
    > "$ART/derived.proc1.txt"
"$PB" seq --version derived --seed "$SEED" --order frame --bodies "$B" --frames "$F" \
    > "$ART/derived.proc2.txt"

# ---------- 证据：调用次序 ----------
for O in frame entity shuffle; do
    "$PB" seq --version derived --seed "$SEED" --order "$O" --bodies "$B" --frames "$F" \
        > "$ART/order_${O}.txt"
done
"$PB" seq --version global --seed "$SEED" --order frame  --bodies "$B" --frames "$F" \
    > "$ART/global_order_frame.txt"
"$PB" seq --version global --seed "$SEED" --order entity --bodies "$B" --frames "$F" \
    > "$ART/global_order_entity.txt"

# ---------- 矩阵 / 雪崩 / 三档指标 ----------
"$PB" matrix    > "$ART/seed_call_matrix.txt"
"$PB" avalanche --samples 20000 > "$ART/avalanche.txt"
"$PB" report --tiers 8,64,512 --frames 64 --samples 16384 \
    --json "$ART/metrics.json" --md "$ART/three_column_table.md"

# ---------- 三档实体数：每档都做一次真正的两进程 cmp，外加次序无关对比 ----------
PC="$ART/process_checks.txt"
: > "$PC"
for TB in 8 64 512; do
    TF=8
    # 仓库相对路径，让证据文件可移植
    P1="artifacts/perturb/process/derived_B${TB}_proc1.txt"
    P2="artifacts/perturb/process/derived_B${TB}_proc2.txt"
    PS="artifacts/perturb/process/derived_B${TB}_shuffle.txt"
    "$PB" seq --version derived --seed "$SEED" --order frame \
        --bodies "$TB" --frames "$TF" --out "$P1"
    "$PB" seq --version derived --seed "$SEED" --order frame \
        --bodies "$TB" --frames "$TF" --out "$P2"
    "$PB" seq --version derived --seed "$SEED" --order shuffle \
        --bodies "$TB" --frames "$TF" --out "$PS"
    if cmp -s "$P1" "$P2"; then CMP=IDENTICAL; else CMP=DIFFER; fi
    H1="$(sha256sum "$P1" | awk '{print $1}')"
    H2="$(sha256sum "$P2" | awk '{print $1}')"
    {
        echo "[tier bodies=$TB frames=$TF items=$((TB*TF*2))]"
        echo "  proc1(order=frame)   sha256=$H1"
        echo "  proc2(order=frame)   sha256=$H2"
        echo "  two-process raw cmp=$CMP"
    } >> "$PC"
    "$PB" compare --a "$P1" --b "$P2" --json >> "$PC"
    # 第三个进程故意用打乱的调用次序，数据行仍须逐项相同
    "$PB" compare --a "$P1" --b "$PS" --json >> "$PC"
done

# ---------- 端到端：goframe run 内置回放自检，两个进程的指纹也必须相同 ----------
"$GB" run --frames 20 --bodies 8 --substeps 2 --gravity 2 \
    --perturb 16384 --seed "$SEED" > "$ART/e2e.proc1.txt" 2>&1
"$GB" run --frames 20 --bodies 8 --substeps 2 --gravity 2 \
    --perturb 16384 --seed "$SEED" > "$ART/e2e.proc2.txt" 2>&1

# ---------- 汇总对比证据 ----------
V="$ART/verification.txt"
R="artifacts/perturb"
{
    echo "goframe 随机扰动复现实验 · 对比汇总"
    echo "seed=$SEED canonical_bodies=$B frames=$F items=$((B*F*2))"
    echo

    echo "=== A. 旧做法：global + 不控制种子，两个独立进程（预期 DIFFER）==="
    "$PB" compare --a "$R/global.auto.proc1.txt" --b "$R/global.auto.proc2.txt"
    if cmp -s "$R/global.auto.proc1.txt" "$R/global.auto.proc2.txt"; then
        echo "raw_cmp=IDENTICAL (异常！)"
    else
        echo "raw_cmp=DIFFER (符合预期：进程随机种子导致对不上)"
    fi
    echo

    echo "=== B. 对照：global + 固定种子/固定次序，两个进程（预期 IDENTICAL）==="
    "$PB" compare --a "$R/global.fixed.proc1.txt" --b "$R/global.fixed.proc2.txt"
    echo

    echo "=== C. 新做法：derived，两个独立进程（预期 IDENTICAL）==="
    "$PB" compare --a "$R/derived.proc1.txt" --b "$R/derived.proc2.txt"
    if cmp -s "$R/derived.proc1.txt" "$R/derived.proc2.txt"; then
        echo "raw_cmp=IDENTICAL (逐字节一致，逐项相同)"
    else
        echo "raw_cmp=DIFFER (异常！)"
    fi
    echo

    echo "=== D. 新做法：同一组 key，frame / entity / shuffle 三种调用次序（预期 identical=true）==="
    "$PB" compare --a "$R/order_frame.txt"  --b "$R/order_entity.txt"
    "$PB" compare --a "$R/order_frame.txt"  --b "$R/order_shuffle.txt"
    echo "数据行直接比对（去掉含 call_order 的 # 头）："
    if diff <(grep -v '^#' "$R/order_frame.txt") \
            <(grep -v '^#' "$R/order_shuffle.txt") >/dev/null; then
        echo "frame_vs_shuffle_data=IDENTICAL"
    else
        echo "frame_vs_shuffle_data=DIFFER (异常！)"
    fi
    echo

    echo "=== E. 旧做法：同种子，frame vs entity 调用次序（预期 DIFFER）==="
    "$PB" compare --a "$R/global_order_frame.txt" --b "$R/global_order_entity.txt"
    echo

    echo "=== F. 两版规范序列之间的差异（不同生成机制，预期高差异分）==="
    "$PB" compare --a "$R/global_sequence.txt" --b "$R/derived_sequence.txt"
    echo

    echo "=== G. 端到端 goframe run（含 replay 自检），两个进程指纹输出 ==="
    if cmp -s "$R/e2e.proc1.txt" "$R/e2e.proc2.txt"; then
        echo "e2e_raw_cmp=IDENTICAL"
    else
        echo "e2e_raw_cmp=DIFFER (异常！)"
    fi
    tail -n 2 "$R/e2e.proc1.txt"
} > "$V" 2>&1 || true

# ---------- 校验和清单（排除清单自身） ----------
(
    cd "$ART"
    find . -type f ! -name checksums.txt -print0 \
        | sort -z \
        | xargs -0 sha256sum > checksums.txt
)

echo "artifacts written to $ART"
