package snap

import "fmt"

// FieldDiff 是单个字段的逐项还原差异。无论相等与否都会产出一条，
// 因此审计者可以看到“逐字段比过”的完整记录，而不只是一个布尔结论。
type FieldDiff struct {
	ID       FieldID `json:"id"`
	Name     string  `json:"name"`
	Full     int32   `json:"full_value"`
	Restored int32   `json:"restored_value"`
	Equal    bool    `json:"equal"`
}

// DiffReport 是按字段逐个比较的完整结果。
type DiffReport struct {
	Total          int         `json:"total_fields"`
	EqualCount     int         `json:"equal_fields"`
	DifferentCount int         `json:"different_fields"`
	AllEqual       bool        `json:"all_equal"`
	Fields         []FieldDiff `json:"fields"`
}

// Compare 以全量旧值 full 为基准，逐字段核对还原值 restored。
// 它刻意不做整体比较（不用 bytes.Equal / DeepEqual），
// 而是按编号顺序对每个字段生成一条 FieldDiff。
func Compare(x *Index, full, restored []int32) (*DiffReport, error) {
	if err := x.checkValues(full); err != nil {
		return nil, fmt.Errorf("full: %w", err)
	}
	if err := x.checkValues(restored); err != nil {
		return nil, fmt.Errorf("restored: %w", err)
	}
	r := &DiffReport{Total: x.N(), Fields: make([]FieldDiff, 0, x.N())}
	for i := 0; i < x.N(); i++ {
		d := FieldDiff{
			ID:       FieldID(i),
			Name:     x.fields[i].Name,
			Full:     full[i],
			Restored: restored[i],
			Equal:    full[i] == restored[i],
		}
		if d.Equal {
			r.EqualCount++
		} else {
			r.DifferentCount++
		}
		r.Fields = append(r.Fields, d)
	}
	r.AllEqual = r.DifferentCount == 0
	return r, nil
}

// CompareSnapshots 是 Compare 的快照包装。
func CompareSnapshots(x *Index, full *Snapshot, restored []int32) (*DiffReport, error) {
	if full == nil {
		return nil, fmt.Errorf("%w: full 快照为空", ErrLength)
	}
	return Compare(x, full.Values, restored)
}

// MismatchIDs 返回所有不一致字段的编号，便于在三行表里给出简明结论。
func (r *DiffReport) MismatchIDs() []FieldID {
	var ids []FieldID
	for _, d := range r.Fields {
		if !d.Equal {
			ids = append(ids, d.ID)
		}
	}
	return ids
}

// SummaryLine 返回“0/32 个字段不一致”这样的表格单元格文本。
func (r *DiffReport) SummaryLine() string {
	if r.AllEqual {
		return fmt.Sprintf("逐字段比对 %d 项，全部一致（不一致 0）", r.Total)
	}
	return fmt.Sprintf("逐字段比对 %d 项，不一致 %d，编号 %v",
		r.Total, r.DifferentCount, r.MismatchIDs())
}
