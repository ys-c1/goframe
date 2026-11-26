package snap

import (
	"errors"
	"fmt"
)

// FieldValue 是全量快照中的一个字段值（JSON 对照用）。
type FieldValue struct {
	ID    FieldID `json:"id"`
	Name  string  `json:"name"`
	Value int32   `json:"value"`
}

// Snapshot 是一帧的全量快照。Values 是比较/还原用的规范数据，
// Fields 是同一份数据的可读展开，ByteSize 是规范二进制体积（见 codec.go）。
type Snapshot struct {
	Kind     string       `json:"kind"`
	Tier     string       `json:"tier,omitempty"`
	Tick     int          `json:"tick"`
	N        int          `json:"field_count"`
	ByteSize int          `json:"binary_byte_size"`
	Fields   []FieldValue `json:"fields"`
	Values   []int32      `json:"-"`
}

// NewSnapshot 把按编号顺序排列的原始值包装成全量快照。
func NewSnapshot(x *Index, tick int, values []int32) (*Snapshot, error) {
	if err := x.checkValues(values); err != nil {
		return nil, err
	}
	vv := append([]int32(nil), values...)
	fields := make([]FieldValue, x.N())
	for i, v := range vv {
		fields[i] = FieldValue{ID: FieldID(i), Name: x.fields[i].Name, Value: v}
	}
	return &Snapshot{
		Kind:     "full",
		Tick:     tick,
		N:        x.N(),
		ByteSize: FullSize(x.N()),
		Fields:   fields,
		Values:   vv,
	}, nil
}

// Entry 是一条回滚差分记录：字段编号 + 该字段改动前的旧值。
// 二元组 (ID, Old) 就是题述的复合定位键。
type Entry struct {
	ID   FieldID `json:"id"`
	Name string  `json:"name"`
	Old  int32   `json:"old_value"`
}

// Delta 是把“改动后状态”还原回“改动前状态”所需的全部条目。
type Delta struct {
	Kind       string  `json:"kind"`
	Tier       string  `json:"tier,omitempty"`
	TickBefore int     `json:"tick_before"`
	N          int     `json:"field_count"`
	ByteSize   int     `json:"binary_byte_size"`
	Entries    []Entry `json:"entries"`
}

// ErrNotLocatable 是复合定位失败的统一错误。
var ErrNotLocatable = errors.New("差分条目无法唯一定位字段")

// Build 比较改动前 oldV 与改动后 newV，为每个发生变化的字段生成一条
// “编号 + 旧值”记录。该差分应用到 newV 上即可还原 oldV。
// 改动量 k 恒等于 len(d.Entries)。
func Build(x *Index, oldV, newV []int32) (*Delta, error) {
	if err := x.checkValues(oldV); err != nil {
		return nil, fmt.Errorf("old: %w", err)
	}
	if err := x.checkValues(newV); err != nil {
		return nil, fmt.Errorf("new: %w", err)
	}
	d := &Delta{Kind: "delta", N: x.N()}
	for i := 0; i < x.N(); i++ {
		if oldV[i] != newV[i] {
			d.Entries = append(d.Entries, Entry{
				ID:   FieldID(i),
				Name: x.fields[i].Name,
				Old:  oldV[i],
			})
		}
	}
	d.ByteSize = DeltaSize(x.N(), len(d.Entries))
	return d, nil
}

// ChangeCount 返回改动字段数 k。
func (d *Delta) ChangeCount() int { return len(d.Entries) }

// Validate 检查差分内部良构：编号不越界、不重复。
// “每个被改动字段恰好一条记录”是唯一还原的必要条件之一。
func (d *Delta) Validate(n int) error {
	if d.N != n {
		return fmt.Errorf("%w: delta.N=%d, index.N=%d", ErrLength, d.N, n)
	}
	seen := make(map[FieldID]int, len(d.Entries))
	for j, e := range d.Entries {
		if int(e.ID) >= n {
			return fmt.Errorf("%w: 第 %d 条 id=%d 越界(N=%d)", ErrBadFieldID, j, e.ID, n)
		}
		if prev, dup := seen[e.ID]; dup {
			return fmt.Errorf("%w: id=%d 在第 %d、%d 条重复", ErrNotLocatable, e.ID, prev, j)
		}
		seen[e.ID] = j
	}
	return nil
}

// RestoreValues 在当前（改动后）值 current 上应用回滚差分，
// 返回还原出的改动前值的新切片，不修改入参。
func (d *Delta) RestoreValues(x *Index, current []int32) ([]int32, error) {
	if err := x.checkValues(current); err != nil {
		return nil, err
	}
	if err := d.Validate(x.N()); err != nil {
		return nil, err
	}
	out := append([]int32(nil), current...)
	for _, e := range d.Entries {
		out[e.ID] = e.Old
	}
	return out, nil
}

// Apply 与 RestoreValues 相同，但直接原地写回 values，
// 供 sim.State 按条目逐字段回写前的校验使用。
func (d *Delta) Apply(x *Index, values []int32) error {
	if err := x.checkValues(values); err != nil {
		return err
	}
	if err := d.Validate(x.N()); err != nil {
		return err
	}
	for _, e := range d.Entries {
		values[e.ID] = e.Old
	}
	return nil
}

// LocateChanged 用“字段编号 id + 旧值 old”在旧全量快照中唯一定位被改字段。
//
// 这是充要条件在代码里的对应物：
//  1. 编号必须在编号表内（编号单射 → 槽位唯一）；
//  2. 该槽位的当前记录值必须等于 old（旧值是要写回的真值，也是校验凭据）；
//  3. 复合键 (id, old) 在快照中必须恰好命中一次，否则返回 ErrNotLocatable。
func (s *Snapshot) LocateChanged(id FieldID, old int32) (int, error) {
	if int(id) >= s.N {
		return -1, fmt.Errorf("%w: id=%d, N=%d", ErrBadFieldID, id, s.N)
	}
	// 编号表单射，(id, old) 至多命中一次；仍按复合键全表核对，
	// 让“恰好一次”这个唯一性条件可执行、可测试。
	for _, f := range s.Fields {
		if f.ID == id {
			if f.Value != old {
				return -1, fmt.Errorf("%w: 编号 %d(%s) 的旧值是 %d，定位键给出 %d",
					ErrNotLocatable, id, f.Name, f.Value, old)
			}
			return int(id), nil
		}
	}
	return -1, fmt.Errorf("%w: 编号 %d 不在快照中", ErrNotLocatable, id)
}

// LocateByOldValue 只凭旧值定位（反例用）：字段值可能重复，
// 一旦多命中即说明“只靠旧值”不满足唯一性。
func (s *Snapshot) LocateByOldValue(old int32) (int, error) {
	hit := -1
	hits := 0
	for _, f := range s.Fields {
		if f.Value == old {
			hits++
			hit = int(f.ID)
		}
	}
	switch hits {
	case 1:
		return hit, nil
	case 0:
		return -1, fmt.Errorf("%w: 旧值 %d 无匹配", ErrNotLocatable, old)
	default:
		return -1, fmt.Errorf("%w: 旧值 %d 命中 %d 个字段，定位不唯一", ErrNotLocatable, old, hits)
	}
}
