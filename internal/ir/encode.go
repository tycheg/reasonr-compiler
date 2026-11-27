package ir

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// The binary IR format: a magic string, a version, then the program. The
// field order is fixed, so the same program gives the same bytes and its
// SHA-256 hash fits rt_schema_versions.
const (
	magic   = "RRIR"
	Version = 3
)

// Encode serializes a program.
func Encode(p *Program) []byte {
	e := &encoder{}
	e.buf = append(e.buf, magic...)
	e.uint(Version)
	e.str(p.Extension)
	e.strs(p.Files)
	e.uint(uint64(len(p.Funcs)))
	for _, f := range p.Funcs {
		e.fn(f)
	}
	e.uint(uint64(len(p.Reactions)))
	for _, r := range p.Reactions {
		e.str(r.Event)
		e.strs(r.Funcs)
	}
	return e.buf
}

// Hash returns the SHA-256 of the serialized program.
func Hash(p *Program) [32]byte { return sha256.Sum256(Encode(p)) }

type encoder struct{ buf []byte }

func (e *encoder) uint(v uint64) { e.buf = binary.AppendUvarint(e.buf, v) }
func (e *encoder) int(v int64)   { e.buf = binary.AppendVarint(e.buf, v) }
func (e *encoder) byte(b byte)   { e.buf = append(e.buf, b) }

func (e *encoder) str(s string) {
	e.uint(uint64(len(s)))
	e.buf = append(e.buf, s...)
}

func (e *encoder) strs(list []string) {
	e.uint(uint64(len(list)))
	for _, s := range list {
		e.str(s)
	}
}

func (e *encoder) bool(b bool) {
	if b {
		e.byte(1)
	} else {
		e.byte(0)
	}
}

func (e *encoder) typ(t *Type) {
	e.byte(byte(t.Kind))
	switch t.Kind {
	case TEnum, TValue, TID, TRef, TRecord:
		e.str(t.Name)
	case TOptional, TList, TSet, TPage:
		e.typ(t.Elem)
	case TMap:
		e.typ(t.Key)
		e.typ(t.Elem)
	}
}

func (e *encoder) fn(f *Func) {
	e.byte(byte(f.Kind))
	e.str(f.Name)
	e.str(f.Owner)
	e.uint(uint64(len(f.Access)))
	for _, a := range f.Access {
		e.byte(byte(a.Kind))
		e.str(a.Name)
	}
	e.uint(uint64(f.Params))
	e.uint(uint64(len(f.Slots)))
	for _, s := range f.Slots {
		e.str(s.Name)
		e.typ(s.Type)
	}
	e.bool(f.Result != nil)
	if f.Result != nil {
		e.typ(f.Result)
	}
	e.uint(uint64(len(f.Consts)))
	for _, c := range f.Consts {
		e.typ(c.Type)
		e.bool(c.None)
		e.str(c.Text)
	}
	e.uint(uint64(len(f.Blocks)))
	for _, b := range f.Blocks {
		e.uint(uint64(len(b.Instrs)))
		for _, in := range b.Instrs {
			e.instr(in)
		}
	}
}

func (e *encoder) instr(in Instr) {
	e.uint(uint64(in.Op))
	e.int(int64(in.Dst))
	e.uint(uint64(len(in.Args)))
	for _, a := range in.Args {
		e.int(int64(a))
	}
	e.str(in.Sym)
	e.strs(in.Names)
	e.str(in.Aux)
	e.uint(uint64(len(in.Targets)))
	for _, t := range in.Targets {
		e.uint(uint64(t))
	}
	e.bool(in.Find != nil)
	if s := in.Find; s != nil {
		e.str(s.Type)
		e.str(s.Index)
		e.uint(uint64(len(s.Where)))
		for _, c := range s.Where {
			e.strs(c.Path)
			e.byte(byte(c.Op))
			e.int(int64(c.Value))
		}
		e.uint(uint64(len(s.Sort)))
		for _, k := range s.Sort {
			e.strs(k.Path)
			e.bool(k.Desc)
		}
		e.int(int64(s.Page))
		e.int(int64(s.Limit))
	}
	e.uint(uint64(in.File))
	e.uint(uint64(in.Line))
}

// Decode parses a serialized program and verifies it (Verify).
func Decode(data []byte) (p *Program, err error) {
	d := &decoder{buf: data}
	defer func() {
		if r := recover(); r != nil {
			if de, ok := r.(decodeError); ok {
				p, err = nil, de.err
				return
			}
			panic(r)
		}
	}()
	if len(data) < len(magic) || string(data[:len(magic)]) != magic {
		return nil, errors.New("ir: this is not ReasonR IR")
	}
	d.off = len(magic)
	if v := d.uint(); v != Version {
		return nil, fmt.Errorf("ir: format version %d, %d is supported", v, Version)
	}
	p = &Program{Extension: d.str(), Files: d.strs()}
	n := d.count()
	for i := 0; i < n; i++ {
		p.Funcs = append(p.Funcs, d.fn())
	}
	n = d.count()
	for i := 0; i < n; i++ {
		p.Reactions = append(p.Reactions, &Reaction{Event: d.str(), Funcs: d.strs()})
	}
	if d.off != len(d.buf) {
		return nil, errors.New("ir: extra bytes at the end")
	}
	if err := Verify(p); err != nil {
		return nil, fmt.Errorf("ir: %w", err)
	}
	return p, nil
}

type decodeError struct{ err error }

type decoder struct {
	buf []byte
	off int
}

func (d *decoder) fail(msg string) {
	panic(decodeError{fmt.Errorf("ir: %s (offset %d)", msg, d.off)})
}

func (d *decoder) uint() uint64 {
	v, n := binary.Uvarint(d.buf[d.off:])
	if n <= 0 {
		d.fail("invalid number")
	}
	d.off += n
	return v
}

func (d *decoder) int() int64 {
	v, n := binary.Varint(d.buf[d.off:])
	if n <= 0 {
		d.fail("invalid number")
	}
	d.off += n
	return v
}

// int32 reads a number within int32.
func (d *decoder) int32() int32 {
	v := d.int()
	if v < -1<<31 || v > 1<<31-1 {
		d.fail("number out of range")
	}
	return int32(v)
}

// count reads a list length: every element takes at least a byte.
func (d *decoder) count() int {
	v := d.uint()
	if v > uint64(len(d.buf)-d.off) {
		d.fail("list too long")
	}
	return int(v)
}

func (d *decoder) byte() byte {
	if d.off >= len(d.buf) {
		d.fail("unexpected end")
	}
	b := d.buf[d.off]
	d.off++
	return b
}

func (d *decoder) bool() bool {
	switch d.byte() {
	case 0:
		return false
	case 1:
		return true
	}
	d.fail("invalid boolean")
	return false
}

func (d *decoder) str() string {
	n := d.count()
	s := string(d.buf[d.off : d.off+n])
	d.off += n
	return s
}

func (d *decoder) strs() []string {
	n := d.count()
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, d.str())
	}
	return out
}

func (d *decoder) typ(depth int) *Type {
	if depth > maxTypeDepth {
		d.fail("type too deep")
	}
	t := &Type{Kind: TypeKind(d.byte())}
	switch t.Kind {
	case TEnum, TValue, TID, TRef, TRecord:
		t.Name = d.str()
	case TOptional, TList, TSet, TPage:
		t.Elem = d.typ(depth + 1)
	case TMap:
		t.Key = d.typ(depth + 1)
		t.Elem = d.typ(depth + 1)
	default:
		if t.Kind >= tKindCount {
			d.fail("unknown type kind")
		}
	}
	return t
}

func (d *decoder) fn() *Func {
	f := &Func{Kind: FuncKind(d.byte()), Name: d.str(), Owner: d.str()}
	n := d.count()
	for i := 0; i < n; i++ {
		f.Access = append(f.Access, Access{Kind: AccessKind(d.byte()), Name: d.str()})
	}
	params := d.uint()
	n = d.count()
	if params > uint64(n) {
		d.fail("more parameters than slots")
	}
	f.Params = int(params)
	for i := 0; i < n; i++ {
		f.Slots = append(f.Slots, Slot{Name: d.str(), Type: d.typ(0)})
	}
	if d.bool() {
		f.Result = d.typ(0)
	}
	n = d.count()
	for i := 0; i < n; i++ {
		f.Consts = append(f.Consts, Const{Type: d.typ(0), None: d.bool(), Text: d.str()})
	}
	n = d.count()
	for i := 0; i < n; i++ {
		b := &Block{}
		m := d.count()
		for j := 0; j < m; j++ {
			b.Instrs = append(b.Instrs, d.instr())
		}
		f.Blocks = append(f.Blocks, b)
	}
	return f
}

func (d *decoder) instr() Instr {
	op := d.uint()
	if op >= uint64(len(ops)) {
		d.fail("unknown operation")
	}
	in := Instr{Op: Op(op), Dst: int(d.int32())}
	n := d.count()
	for i := 0; i < n; i++ {
		in.Args = append(in.Args, Operand(d.int32()))
	}
	in.Sym = d.str()
	in.Names = d.strs()
	in.Aux = d.str()
	n = d.count()
	for i := 0; i < n; i++ {
		t := d.uint()
		if t > 1<<31-1 {
			d.fail("block number out of range")
		}
		in.Targets = append(in.Targets, int(t))
	}
	if d.bool() {
		s := &FindSpec{Type: d.str(), Index: d.str()}
		n = d.count()
		for i := 0; i < n; i++ {
			s.Where = append(s.Where, Cond{Path: d.strs(), Op: CondOp(d.byte()), Value: Operand(d.int32())})
		}
		n = d.count()
		for i := 0; i < n; i++ {
			s.Sort = append(s.Sort, SortKey{Path: d.strs(), Desc: d.bool()})
		}
		s.Page = Operand(d.int32())
		s.Limit = Operand(d.int32())
		in.Find = s
	}
	file, line := d.uint(), d.uint()
	if file > 1<<31-1 || line > 1<<31-1 {
		d.fail("position out of range")
	}
	in.File, in.Line = int(file), int(line)
	return in
}
