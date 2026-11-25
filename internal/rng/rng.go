// Package rng 提供回放安全的确定性随机源。
//
// 回放要求“同一种子 + 同一逻辑输入”在任意进程、任意机器上复现同一条扰动序列，
// 因此禁止使用 math/rand 顶层全局源（Go 1.20 起进程启动随机播种），也禁止让
// 实体的随机数依赖“它前面的实体调用了多少次”。
//
// 本包的做法是计数器模式的子序列派生：
//
//	u = Derive(masterSeed, entityID, purposeTag, counter)
//
// Derive 是纯函数，只使用固定宽度 uint64 运算与 SplitMix64 终混合（fmix64，
// 强雪崩），所以与调用进程、调用次序、调用次数全部无关，并支持按 key 随机访问。
package rng

import (
	"math/rand"

	"goframe/internal/fixed"
)

// Tag 是用途/流域分隔标识。不同用途必须使用不同 Tag，
// 保证即使实体编号与计数器相同，输出也不会重用。
type Tag uint64

// 用途标签取互不相同的非零常量（ASCII 助记）。
const (
	TagJitterX Tag = 0x4A5458 // "JTX"，速度扰动 X 轴
	TagJitterY Tag = 0x4A5459 // "JTY"，速度扰动 Y 轴
)

// domainSalt 是本包的固定域分隔盐，避免派生结果与裸 SplitMix64 序列同构。
const domainSalt uint64 = 0x676F6672616D6521 // "goframe!"

// fmix64 是 SplitMix64 的终混合器（Stafford variant 13）。
// 设计目标是强雪崩：输入翻转 1 bit，输出约一半 bit 翻转。
func fmix64(z uint64) uint64 {
	z += 0x9E3779B97F4A7C15
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Derive 返回元组 (master, entity, tag, counter) 对应的确定性子序列项。
//
// 同一个元组在任意进程/机器/调用次序下必然得到同一个值；不同元组经过三级
// fmix64 雪崩混合，输出在计算意义上不可区分于独立均匀随机数。
func Derive(master uint64, entity int, tag Tag, counter uint64) uint64 {
	// 每一级都把完整输入混进雪崩函数，而不是简单异或，避免元组分量之间
	// 出现线性抵消（例如 entity 相同、tag 异或为 0 之类的结构相关性）。
	h := fmix64(master ^ domainSalt)
	h = fmix64(h ^ fmix64(uint64(entity)) ^ (uint64(tag) * 0x9E3779B97F4A7C15))
	h = fmix64(h ^ fmix64(counter) ^ 0xD1B54A32D192ED03)
	return h
}

// Stream 是某个 (实体, 用途) 子序列的有状态只读游标。
// 它推进的是自己的计数器，不与任何其他实体共享状态。
type Stream struct {
	master  uint64
	entity  int
	tag     Tag
	counter uint64
}

// NewStream 派生实体 entity 在用途 tag 下的子序列，计数器从 0 开始。
func NewStream(master uint64, entity int, tag Tag) *Stream {
	return &Stream{master: master, entity: entity, tag: tag}
}

// Uint64 返回子序列下一项。
func (s *Stream) Uint64() uint64 {
	v := Derive(s.master, s.entity, s.tag, s.counter)
	s.counter++
	return v
}

// Uint64At 不推进游标，直接返回第 counter 项（随机访问，次序无关）。
func (s *Stream) Uint64At(counter uint64) uint64 {
	return Derive(s.master, s.entity, s.tag, counter)
}

// Jitter 返回第 counter 次、幅度 amp（Q16.16 原始单位，amp>=0）的定点扰动，
// 结果落在 [-amp, amp]。全程整数运算，跨平台一致。
func Jitter(master uint64, entity int, tag Tag, counter uint64, amp int32) fixed.Value {
	if amp <= 0 {
		return 0
	}
	return MapJitter(Derive(master, entity, tag, counter), amp)
}

// MapJitter 把一个均匀 uint64 映射到 [-amp, amp] 的 Q16.16 定点扰动。
// 全局源基线与派生源共用同一映射，保证两版序列只有“随机数来源”不同。
func MapJitter(r uint64, amp int32) fixed.Value {
	if amp <= 0 {
		return 0
	}
	// 取高 32 位（均匀），线性映射到 [0, amp]，再平移到 [-amp, amp]。
	d := int64(uint64(uint32(r>>32)) * uint64(amp) >> 32)
	return fixed.FromRaw(int(2*d - int64(amp)))
}

// ---- 以下是“旧做法”的基线，仅用于反证实验，禁止接入回放路径 ----

// GlobalUint64 走 math/rand 顶层全局源。
// Go 1.20+ 该源在每个进程启动时用随机种子初始化，因此换进程必然对不上。
func GlobalUint64() uint64 { return rand.Uint64() }

// SharedSource 模拟“所有实体共用一个手工播种的 rand.Rand”的旧做法。
// 即使种子固定，某个实体第 k 次取到的值仍取决于此前全源被调用的总次数，
// 也就是取决于其他实体的调用次序与额外调用次数。
type SharedSource struct {
	r *rand.Rand
}

// NewSharedSource 用固定种子创建共享源。
func NewSharedSource(seed int64) *SharedSource {
	return &SharedSource{r: rand.New(rand.NewSource(seed))}
}

// Uint64 推进共享源并返回一项。
func (s *SharedSource) Uint64() uint64 { return s.r.Uint64() }
