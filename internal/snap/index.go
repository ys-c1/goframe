// Package snap 提供回滚用的全量快照与差分快照。
//
// 设计要点：
//   - 每个标量字段都有稳定且唯一的 FieldID（字段编号），编号顺序与
//     sim.State.Raw() 的导出顺序严格一致；
//   - 回滚差分的每一条目是“字段编号 + 旧值”（Entry），这正是差分可唯一
//     还原的复合定位键；
//   - 二进制编码全部定宽、小端、无填充，体积只取决于字段总数 N 与改动量 k，
//     因而可以给出精确的体积上界（见 bound.go）；
//   - 还原结果与全量快照的比较必须走 Compare，逐字段产出 FieldDiff，
//     不允许用整体比较代替。
package snap

import (
	"errors"
	"fmt"
)

// FieldID 是稳定的字段编号，线上用 uint16 编码（2 字节）。
type FieldID uint16

// MaxFieldID 是 uint16 编号上限。
const MaxFieldID = int(^FieldID(0)) // 65535

// FieldsPerBody 是每个实体的标量字段数：posX/posY/velX/velY。
const FieldsPerBody = 4

// Field 描述一个逻辑标量字段。
type Field struct {
	ID   FieldID `json:"id"`
	Name string  `json:"name"`
}

// Index 是不可变的字段编号表：编号 0..N-1 唯一且稳定。
type Index struct {
	bodies int
	fields []Field
}

// 编号越界等错误。
var (
	ErrBodies     = errors.New("实体数非法")
	ErrLength     = errors.New("快照值长度与字段编号表不一致")
	ErrBadFieldID = errors.New("字段编号越界")
)

// NewIndex 按实体数 n 创建编号表。
// 编号约定与 sim.State.Raw 完全一致：id=4*i+k，
// k=0 posX、1 posY、2 velX、3 velY。
func NewIndex(bodies int) (*Index, error) {
	if bodies <= 0 {
		return nil, fmt.Errorf("%w: bodies=%d", ErrBodies, bodies)
	}
	if FieldsPerBody*bodies-1 > MaxFieldID {
		return nil, fmt.Errorf("%w: bodies=%d 超出 uint16 编号空间", ErrBodies, bodies)
	}
	names := [FieldsPerBody]string{"posX", "posY", "velX", "velY"}
	fs := make([]Field, 0, FieldsPerBody*bodies)
	for i := 0; i < bodies; i++ {
		for k, nm := range names {
			id := FieldsPerBody*i + k
			fs = append(fs, Field{ID: FieldID(id), Name: fmt.Sprintf("b%d.%s", i, nm)})
		}
	}
	return &Index{bodies: bodies, fields: fs}, nil
}

// Bodies 返回实体数。
func (x *Index) Bodies() int { return x.bodies }

// N 返回标量字段总数 N=4*bodies。
func (x *Index) N() int { return len(x.fields) }

// Fields 返回编号表的副本。
func (x *Index) Fields() []Field {
	out := make([]Field, len(x.fields))
	copy(out, x.fields)
	return out
}

// Name 返回编号对应的字段名，越界时返回 "?"。
func (x *Index) Name(id FieldID) string {
	if int(id) < 0 || int(id) >= len(x.fields) {
		return "?"
	}
	return x.fields[id].Name
}

// Locate 仅凭字段编号定位槽位。编号表是单射，因此槽位唯一。
func (x *Index) Locate(id FieldID) (int, error) {
	if int(id) >= len(x.fields) {
		return -1, fmt.Errorf("%w: id=%d, N=%d", ErrBadFieldID, id, len(x.fields))
	}
	return int(id), nil
}

// checkValues 校验值切片长度。
func (x *Index) checkValues(v []int32) error {
	if len(v) != len(x.fields) {
		return fmt.Errorf("%w: got=%d, want=%d", ErrLength, len(v), len(x.fields))
	}
	return nil
}
