# 子步数无关性：旧 Euler 的 `G·h²` 偏差与中点格式修复

本文回答四件事：

1. 证明修复前的积分格式与子步数相关，并把 1 档 / 4 档的差写成**重力乘子步长二阶项**；
2. 证明改用显式中点格式后，同一帧末态与子步数无关；
3. 给出无关性成立的（数学与定点逐位）条件；
4. 给出可复现的验证、逐帧“哈希差值为零”专检与回放记录位置。

所有结论都有可执行测试和 `artifacts/` 下的实跑证据支撑。

---

## 1. 结论速览

- **修复前（初始提交 `3bb65f9` 的半隐式 Euler）确实与子步数相关。**
  同一段 20 帧指令分别用 `--substeps 1` 和 `--substeps 4` 跑，**20 个帧哈希两两不同，相同帧数为 0**。
  见 `artifacts/cli/before_euler_sub1.txt`、`before_euler_sub4.txt` 与
  `artifacts/euler_dependence_proof.json`。
- 重力造成的分裂误差，其每个子步的基本项正是 `(k+1)·G·h²`（重力 × 子步长二阶项）。
  1 档与 4 档在一帧内的重力位移差为 `3G/8 = 6·(G·h₄²)`，即子步长二阶项的整数三角倍。
- **改用显式中点格式并以宽精度保留保护位后，子步数 1/2/4 三档（以及边界档 8、上限档 16）
  的同一帧末态哈希完全相同，逐帧哈希差值恒为 0，原始状态切片也逐分量相同。**
  见 `artifacts/substep_invariance.json`、`artifacts/verification_summary.json`
  与 `artifacts/cli/crosscheck.txt`。
- 回放记录存于 **`artifacts/replay.json`**（同内容副本 `artifacts/replay_record.json`），
  `Record` 与 `Playback` 的逐帧指纹一致。

---

## 2. 旧格式为什么与子步数相关

把一帧长度归一化为 `1`，子步数记为 `n`，子步长 `h = 1/n`。
设一维（竖直）常量重力为 `G`，帧初位置、速度为 `y₀, v₀`。

### 2.1 仓库里的逐字实现：比标准 Euler 还少一个 `h`

修复前 `Integrate` 做的事是

```text
g = G / n = G·h
v ← v + g
y ← y + v        // 注意：漂移没有乘子步长 h
```

第 `k` 个子步后速度为 `v_k = v₀ + k·G·h`，于是一帧 `n` 个子步后

```text
y' = y₀ + Σ_{k=1..n} (v₀ + k·G/n)
   = y₀ + n·v₀ + (G/n)·n(n+1)/2
   = y₀ + n·v₀ + G(n+1)/2
v' = v₀ + G
x' = x₀ + n·v_x
```

因此逐字代码的帧映射天然带 `n`：即使 `G=0`，位置仍有 `n·v` 项（漂移漏乘 `h`）。
对“单实体、静止、只跑一帧”，`v₀=0`，有闭式

```text
y_n = G(n+1)/2
```

实测 Q16.16 原始值（`G=2`，`artifacts/euler_dependence_proof.json`）：

| n | y 世界值 `G(n+1)/2` | y 原始值 |
|---|--------------------:|---------:|
| 1 | `2`   | `131072` |
| 2 | `3`   | `196608` |
| 4 | `5`   | `327680` |
| 8 | `9`   | `589824` |

1 档减 4 档：`y₁ − y₄ = G − 5G/2 = −3G/2`（`G=2` 时原始差 `-196608`），与实测逐位一致。
这解释了为什么旧 CLI 的 `sub=1` 与 `sub=4` 哈希在每一帧都不同。

> 这一层“漏乘漂移 `h`”是额外的、比二阶项更大的相关性来源；改成任何自洽的子步格式
> （包括中点）都必须把 `h` 补回漂移项。下面把格式写成带正确 `h` 的标准形式，
> 以精确给出题目所指的“重力 × 子步长二阶项”。

### 2.2 `G·h²` 二阶项与 1/4 两档之差

给半隐式 Euler 补上正确的漂移权重 `h`：

```text
v_{k+1} = v_k + G·h
y_{k+1} = y_k + h·v_{k+1}
```

其中 `v_k = v₀ + k·G·h`。第 `k` 个子步里，重力对位置的贡献是

```text
h · ( (k+1)·G·h ) = (k+1)·G·h²
```

这正是**重力乘子步长的二阶项 `G·h²`，并带一个线性的三角系数 `(k+1)`**。
`n` 步求和：

```text
Δy_G = G·h² · Σ_{j=1..n} j
     = G·h² · n(n+1)/2
     = G(n+1)/(2n)
     = (G/2)(1 + h)
```

相对常量加速的精确位移 `G/2`，多出 `(G/2)·h`，所以全局误差是一阶的，
而其离散来源是每子步的二阶局部项 `G·h²`。任意两档 `p、q` 的差为

```text
Δy_G(p) − Δy_G(q) = (G/2)(1/p − 1/q)
```

取 `p=1, q=4`：

```text
差 = (G/2)(1 − 1/4) = 3G/8
```

令 4 档的子步长 `h₄ = 1/4`，则 `G·h₄² = G/16`，于是

```text
3G/8 = 6·(G/16) = 6·(G·h₄²)
```

即 **1 档与 4 档的差恰好是“重力 × 子步长（`1/4`）二阶项”的 6 倍**
（三角系数：4 档 `Σ_{1..4}j=10` 个 `G h₄²`，而 1 档的 `G` 折合 `16` 个 `G h₄²`，差 `16−10=6`）。
该恒等式由 `verify.TestEuler_Dependence_And_GravityH2Term` 用整数定点数逐位钉住。

---

## 3. 中点格式为什么与子步数无关

显式中点格式（常量加速度 `G`），子步长 `h=1/n`：

```text
v_mid = v_k + G·h/2
y_{k+1} = y_k + h·v_mid
v_{k+1} = v_k + G·h
```

仍有 `v_k = v₀ + k·G·h`。第 `k` 步的位移为

```text
Δy_k = h·(v₀ + k·G·h + G·h/2)
     = h·v₀ + G·h²·(k + 1/2)
```

对 `k=0..n−1` 求和：

```text
Δy   = n·h·v₀ + G·h²·( n(n−1)/2 + n/2 )
     = v₀     + G·h²·n²/2
     = v₀     + G/2            （因为 n·h = 1）
Δv   = n·G·h = G
```

所以整帧映射严格望远镜相消为

```text
x' = x + v_x
y' = y + v_y + G/2
v_y' = v_y + G
```

**式中不再出现 `n`**：无论切成多少个均匀子步，精确算术下同一帧末态完全相同。
对常量加速度，中点（以及梯形/速度 Verlet）给出的就是常量加速运动的精确二次更新。

---

## 4. 无关性成立的条件

“与子步数无关”不是无条件的。本仓库要求下列条件同时成立。

### 4.1 数学/物理条件（精确实数算术）

1. **帧内加速度恒定且不依赖状态**：本内核只有常量重力 `G`（`a_x=0, a_y=G`）。
   若有力随位置/速度/时间变化，中点只是二阶近似，`n` 不同结果仍会不同。
2. **外部指令只在帧首施加**：冲量在进入子步循环前一次性加入速度，帧内没有中途事件。
3. **均匀分割**：每个子步都是 `h=1/n`，且都执行同一个中点格式。
4. **帧内没有间断**：没有碰撞/接触、边界钳制、传送、回绕等非光滑事件；
   这类事件落在不同子步位置会改变结果。
5. **不溢出**：数值始终处在表示范围内。

### 4.2 定点“逐位哈希相同”的额外条件

哈希混合的是 Q16.16 的原始整数，所以还必须保证**不引入随 `n` 变化的舍入**。
有两种实现口径：

- **教科书式每子步立即量化回 Q16.16**：要求 `n` 是整除 `2^16=65536` 的 2 的幂，
  且速度/重力原始值对 `n`（中点半踢还涉及 `2n`）低位对齐，使 `h`、`h/2`、
  `v_mid·h`、`G·h` 的移位全部精确，并保证 `int32` 中间值不溢出。
  仓库的规范指令（整数重力 `G<<16`，冲量 `1<<14 / 1<<15 / 1<<16`）在 `n=1/2/4/8` 下满足。

- **本仓库实际实现（更强，见 `internal/sim/sim.go`）**：`State` 内部另存一份
  **Q(16+16) 的 `int64` 宽精度权威状态**（`guardBits=16`），子步除法在宽域进行，
  只在 `Raw()`/哈希观察时向下量化一次回 Q16.16。对**任意不对齐的 Q16 原始冲量**，
  只要 `n=2^m` 且 `m≤7`（`n≤128`；`Options` 允许的上限是 16，因此
  **`1/2/4/8/16` 全部在内**），`G/n`、`G/(2n)`、`v/n`、`v_mid/n` 这些整数除法
  都逐位精确，宽域末态严格等于上面的闭式帧映射，于是导出的 Q16.16 与哈希逐位相同。
  仍需满足第 4.1 的常量加速/无间断条件，且 `int64` 宽域不溢出。

### 4.3 非 2 的幂档位不保证

`n=3,5,6,7,…` 时 `1/n` 不是二进制有限表示，逐子步整数除会截断；误差会在宽域状态中
跨帧累积，最终可能跨过 Q16.16 的量化边界，使哈希随 `n` 变化。对 1500 组随机输入、
每组 120 帧的对拍显示：`{1,2,4,8,16}` 零分歧，而 `3,5,6,7,9,…,15` 出现分歧。
因此“无关性”明确只对**整除定点尺度的 2 的幂子步档**承诺；题目要求的 `1/2/4` 与边界 `8`
恰好全部满足。

---

## 5. 改了什么

- `internal/sim/sim.go`
  - `Integrate(sub)` 改为在宽精度域执行**一个** `h=1/sub` 的显式中点子步；
    `api` 原本的 `for i:=0;i<SubSteps;i++ { Integrate(SubSteps) }` 结构保持不变。
  - 新增内部宽域字段与 `sync`，`Raw()` 的 Q16.16 导出格式、`hash.State` 完全不变。
  - 旧 Euler 逐字保留为 `IntegrateEuler(sub)`，专供相关性证明/对拍，不在新 `Step` 路径上。
- `api/api_test.go`：把原来“断言子步数会改变结果”的表征测试更新为同时钉住
  “旧 Euler 仍相关”和“新中点 1/4 一致”。
- `verify/`（新包）：统一的指令流、逐帧比对、`G·h²` 证明数据、回放记录结构，
  以及下列硬性测试。
- `cmd/genartifacts/`（新命令）：实跑并写出 `artifacts/` 下全部 JSON 证据。
- `cmd/goframe` 新增 `crosscheck` 子命令：逐帧打印 `sub=1 vs sub=4` 的 `delta`。

---

## 6. 如何复现验证

```sh
make test          # go vet ./... && go test ./...（含全部硬性检查）

# 逐帧“sub=1 vs sub=4 哈希差值为零”专检 + 1/2/4 与边界 8：
go test ./verify/ -run 'TestInvariance_One_Two_Four|TestFrameByFrame|TestSubStep8' -v

# 终端逐帧 delta 表：
go run ./cmd/goframe crosscheck --frames 20 --bodies 8 --gravity 2

# 重新生成 artifacts/*.json：
go run ./cmd/genartifacts

# 直观对比 CLI 哈希：
for n in 1 2 4 8; do go run ./cmd/goframe run --frames 20 --bodies 8 \
    --substeps $n --gravity 2 | grep '^tick='; done
```

对应测试与要求的映射：

| 要求 | 位置 |
|---|---|
| 覆盖子步数 1/2/4，同一帧哈希完全相同，差一帧不过 | `verify.TestInvariance_One_Two_Four_SameFrameHashes` |
| 两档逐帧比对、哈希差值为零**单独验** | `verify.TestFrameByFrame_HashDifferenceZero_TwoTiers` |
| 子步数 8 作为边界再跑一次 | `verify.TestSubStep8_Boundary_Identical` |
| 原始状态逐分量也一致 | `verify.TestAllTiers_RawSnapshotsIdentical` |
| 任意不对齐输入的稳健性（含 16 档） | `verify.TestSubStepInvariance_SeededStreams` |
| 旧 Euler 相关性 + `G·h²` 恒等式 | `verify.TestEuler_Dependence_And_GravityH2Term` |
| 回放 Record/Playback 一致 | `verify.TestReplay_RecordAndPlaybackIdentical` |

判定是逐帧严格相等：任一帧 `hash_sub[a][tick] != hash_sub[b][tick]` 即 `t.Fatal`，
不存在“整体相同/近似通过”。

---

## 7. 回放记录

`artifacts/replay.json` 由 `replay.Record(api.Options{Bodies:8, SubSteps:4, Gravity:2}, CanonicalFrames(20,8))`
真实生成，内含完整 `log.options` 与 `log.frames`（可直接再次 `replay.Playback`），
以及 `recorded_hashes`、`playback_hashes` 和 `replay_identical: true`。
`artifacts/cli/*` 保存修复前（原始提交二进制）与修复后各档 CLI 的完整输出和 `crosscheck.txt`。
