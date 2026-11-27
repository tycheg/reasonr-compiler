package bytecode

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// The binary program format: "RRBC", the format version, the schema in
// JSON, the constant pool, functions, reactions. Numbers are uvarint
// (signed ones varint), strings and slices are length-prefixed, code words
// are 4 bytes little-endian. Encoding is deterministic: the same programs
// give the same bytes.

var magic = []byte("RRBC")

// Encode serializes a program.
func Encode(p *Program) ([]byte, error) {
	schema, err := p.Schema.MarshalIndent()
	if err != nil {
		return nil, err
	}
	w := &writer{buf: append([]byte(nil), magic...)}
	w.uint(FormatVersion)
	w.bytes(schema)
	w.uint(uint64(len(p.Files)))
	for _, f := range p.Files {
		w.str(f)
	}
	w.uint(uint64(len(p.Consts)))
	for _, c := range p.Consts {
		w.typ(&c.Type)
		w.literal(&c.Lit)
	}
	w.uint(uint64(len(p.Funcs)))
	for i := range p.Funcs {
		w.fn(&p.Funcs[i])
	}
	w.uint(uint64(len(p.Reactions)))
	for _, r := range p.Reactions {
		w.uint(uint64(r.Type))
		w.uint(uint64(r.Event))
		w.uint(uint64(len(r.Funcs)))
		for _, f := range r.Funcs {
			w.int(int64(f))
		}
	}
	return w.buf, nil
}

// Hash returns the SHA-256 of the serialized program.
func Hash(p *Program) ([32]byte, error) {
	data, err := Encode(p)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

type writer struct{ buf []byte }

func (w *writer) uint(x uint64)  { w.buf = binary.AppendUvarint(w.buf, x) }
func (w *writer) int(x int64)    { w.buf = binary.AppendVarint(w.buf, x) }
func (w *writer) bytes(b []byte) { w.uint(uint64(len(b))); w.buf = append(w.buf, b...) }
func (w *writer) str(s string)   { w.uint(uint64(len(s))); w.buf = append(w.buf, s...) }

func (w *writer) bool(b bool) {
	if b {
		w.buf = append(w.buf, 1)
	} else {
		w.buf = append(w.buf, 0)
	}
}

func (w *writer) typ(t *model.Type) {
	w.uint(uint64(t.Kind))
	w.uint(uint64(t.Ref))
	w.bool(t.Optional)
	w.bool(t.Elem != nil)
	if t.Elem != nil {
		w.typ(t.Elem)
	}
	w.bool(t.Key != nil)
	if t.Key != nil {
		w.typ(t.Key)
	}
}

func (w *writer) literal(l *model.Literal) {
	w.uint(uint64(l.Kind))
	w.str(l.Text)
	w.uint(uint64(len(l.Fields)))
	for i := range l.Fields {
		w.literal(&l.Fields[i])
	}
	w.uint(uint64(len(l.Elems)))
	for i := range l.Elems {
		w.literal(&l.Elems[i])
	}
}

func (w *writer) fields(fs []model.FieldRef) {
	w.bool(fs != nil)
	w.uint(uint64(len(fs)))
	for _, f := range fs {
		w.uint(uint64(f))
	}
}

func (w *writer) fn(f *Func) {
	w.str(f.Name)
	w.uint(uint64(f.Kind))
	w.uint(uint64(f.Owner))
	w.uint(uint64(f.Method))
	w.uint(uint64(f.Params))
	w.uint(uint64(len(f.Regs)))
	for i := range f.Regs {
		w.typ(&f.Regs[i])
		name := ""
		if i < len(f.RegNames) {
			name = f.RegNames[i]
		}
		w.str(name)
	}
	w.uint(uint64(len(f.Consts)))
	for _, c := range f.Consts {
		w.uint(uint64(c))
	}
	w.bool(f.Result != nil)
	if f.Result != nil {
		w.typ(f.Result)
	}
	w.uint(uint64(len(f.Code)))
	for _, c := range f.Code {
		w.buf = binary.LittleEndian.AppendUint32(w.buf, c)
	}
	w.uint(uint64(len(f.Sites)))
	for _, s := range f.Sites {
		w.uint(uint64(s.Type))
		w.uint(uint64(s.Method))
		w.int(int64(s.Func))
		w.uint(uint64(len(s.Args)))
		for _, a := range s.Args {
			w.uint(uint64(a))
		}
		w.fields(s.Fields)
		w.str(s.Name)
		w.str(s.Role)
	}
	w.uint(uint64(len(f.Finds)))
	for _, fd := range f.Finds {
		w.uint(uint64(fd.Type))
		w.int(int64(fd.Index))
		w.uint(uint64(len(fd.Where)))
		for _, c := range fd.Where {
			w.fields(c.Field)
			w.uint(uint64(c.Op))
			w.uint(uint64(c.Value))
		}
		w.uint(uint64(len(fd.Sort)))
		for _, k := range fd.Sort {
			w.fields(k.Field)
			w.bool(k.Desc)
		}
		w.uint(uint64(fd.Page))
		w.uint(uint64(fd.Limit))
	}
	w.uint(uint64(len(f.Lines)))
	for _, l := range f.Lines {
		w.uint(uint64(l.PC))
		w.uint(uint64(l.File))
		w.uint(uint64(l.Line))
	}
}

// ---------------------------------------------------------------------------

// ErrFormat: the data is not a program of this format version.
var ErrFormat = errors.New("bytecode: invalid program format")

// Decode reads a program. The structure is checked; Verify checks the code.
func Decode(data []byte) (*Program, error) {
	if len(data) < len(magic) || string(data[:len(magic)]) != string(magic) {
		return nil, fmt.Errorf("%w: no RRBC header", ErrFormat)
	}
	r := &reader{data: data, pos: len(magic)}
	if v := r.uint(); r.err == nil && v != FormatVersion {
		return nil, fmt.Errorf("%w: version %d, %d is supported", ErrFormat, v, FormatVersion)
	}
	p := &Program{}
	schema := r.bytes()
	if r.err != nil {
		return nil, r.err
	}
	s, err := model.ParseSchema(schema)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	p.Schema = s
	for range r.count(1) {
		p.Files = append(p.Files, r.str())
	}
	for range r.count(2) {
		var c Const
		c.Type = r.typ(0)
		c.Lit = r.literal(0)
		p.Consts = append(p.Consts, c)
	}
	for range r.count(8) {
		p.Funcs = append(p.Funcs, r.fn())
	}
	for range r.count(3) {
		rc := Reaction{Type: model.TypeRef(r.u32()), Event: model.MethodRef(r.u32())}
		for range r.count(1) {
			rc.Funcs = append(rc.Funcs, r.i32())
		}
		p.Reactions = append(p.Reactions, rc)
	}
	if r.err == nil && r.pos != len(r.data) {
		r.fail("%d extra bytes", len(r.data)-r.pos)
	}
	if r.err != nil {
		return nil, r.err
	}
	return p, nil
}

type reader struct {
	data []byte
	pos  int
	err  error
}

func (r *reader) fail(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf("%w: %s (byte %d)", ErrFormat, fmt.Sprintf(format, args...), r.pos)
	}
}

func (r *reader) uint() uint64 {
	if r.err != nil {
		return 0
	}
	x, n := binary.Uvarint(r.data[r.pos:])
	if n <= 0 {
		r.fail("invalid number")
		return 0
	}
	r.pos += n
	return x
}

func (r *reader) int() int64 {
	if r.err != nil {
		return 0
	}
	x, n := binary.Varint(r.data[r.pos:])
	if n <= 0 {
		r.fail("invalid number")
		return 0
	}
	r.pos += n
	return x
}

func (r *reader) u32() uint32 {
	x := r.uint()
	if x > 1<<32-1 {
		r.fail("number %d exceeds 32 bits", x)
	}
	return uint32(x)
}

func (r *reader) u16() uint16 {
	x := r.uint()
	if x > 1<<16-1 {
		r.fail("number %d exceeds 16 bits", x)
	}
	return uint16(x)
}

func (r *reader) i32() int32 {
	x := r.int()
	if x < -1<<31 || x > 1<<31-1 {
		r.fail("number %d exceeds 32 bits", x)
	}
	return int32(x)
}

func (r *reader) u8() uint8 {
	x := r.uint()
	if x > 255 {
		r.fail("number %d exceeds a byte", x)
	}
	return uint8(x)
}

// count reads a number of elements; each takes at least min bytes.
func (r *reader) count(min int) int {
	n := r.uint()
	if r.err == nil && n > uint64(len(r.data)-r.pos)/uint64(min) {
		r.fail("element count %d exceeds the data", n)
	}
	if r.err != nil {
		return 0
	}
	return int(n)
}

func (r *reader) bytes() []byte {
	n := r.count(1)
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b
}

func (r *reader) str() string { return string(r.bytes()) }

func (r *reader) bool() bool {
	if r.err != nil {
		return false
	}
	if r.pos >= len(r.data) || r.data[r.pos] > 1 {
		r.fail("invalid flag")
		return false
	}
	r.pos++
	return r.data[r.pos-1] == 1
}

const maxDepth = 32

func (r *reader) typ(depth int) model.Type {
	if depth > maxDepth {
		r.fail("type too deep")
		return model.Type{}
	}
	t := model.Type{Kind: model.Kind(r.u8()), Ref: model.TypeRef(r.u32()), Optional: r.bool()}
	if r.bool() {
		e := r.typ(depth + 1)
		t.Elem = &e
	}
	if r.bool() {
		k := r.typ(depth + 1)
		t.Key = &k
	}
	return t
}

func (r *reader) literal(depth int) model.Literal {
	if depth > maxDepth {
		r.fail("literal too deep")
		return model.Literal{}
	}
	l := model.Literal{Kind: model.Kind(r.u8()), Text: r.str()}
	for range r.count(3) {
		l.Fields = append(l.Fields, r.literal(depth+1))
	}
	for range r.count(3) {
		l.Elems = append(l.Elems, r.literal(depth+1))
	}
	return l
}

func (r *reader) fields() []model.FieldRef {
	present := r.bool()
	n := r.count(1)
	if !present {
		if n != 0 {
			r.fail("fields without a flag")
		}
		return nil
	}
	out := make([]model.FieldRef, 0, n)
	for range n {
		out = append(out, model.FieldRef(r.u16()))
	}
	return out
}

func (r *reader) fn() Func {
	f := Func{Name: r.str(), Kind: FuncKind(r.u8()), Owner: model.TypeRef(r.u32()), Method: model.MethodRef(r.u32())}
	f.Params = int(r.u32())
	for range r.count(5) {
		f.Regs = append(f.Regs, r.typ(0))
		f.RegNames = append(f.RegNames, r.str())
	}
	for range r.count(1) {
		f.Consts = append(f.Consts, r.u32())
	}
	if r.bool() {
		t := r.typ(0)
		f.Result = &t
	}
	n := r.count(4)
	for range n {
		f.Code = append(f.Code, binary.LittleEndian.Uint32(r.data[r.pos:]))
		r.pos += 4
	}
	for range r.count(7) {
		s := Site{Type: model.TypeRef(r.u32()), Method: model.MethodRef(r.u32()), Func: r.i32()}
		for range r.count(1) {
			s.Args = append(s.Args, r.u32())
		}
		s.Fields = r.fields()
		s.Name = r.str()
		s.Role = r.str()
		f.Sites = append(f.Sites, s)
	}
	for range r.count(6) {
		fd := Find{Type: model.TypeRef(r.u32()), Index: r.i32()}
		for range r.count(4) {
			fd.Where = append(fd.Where, Cond{Field: r.fields(), Op: model.CondOp(r.u8()), Value: r.u32()})
		}
		for range r.count(3) {
			fd.Sort = append(fd.Sort, model.SortKey{Field: r.fields(), Desc: r.bool()})
		}
		fd.Page = r.u32()
		fd.Limit = r.u32()
		f.Finds = append(f.Finds, fd)
	}
	for range r.count(3) {
		f.Lines = append(f.Lines, Line{PC: r.u32(), File: r.u32(), Line: r.u32()})
	}
	return f
}
