package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/token"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

var (
	textTypeNotValue = "%q is a type, not a value"
	textEmptyList    = "an empty list"
	textEmptyMap     = "an empty map"
	textNotEnumValue = "value %q"
	textNoField      = "field %q"
	textSearchOnly   = "search with [Type]: search is only allowed in queries"
	textAllOnly      = "all only iterates entities in migrations"
	textNewWhy       = "new ids are created by commands"
	hintEnumValue    = "this is a value of enum %s: write %s.%s"
	kindGenitive     = map[ObjKind]string{
		ObjLocal:   "variable",
		ObjParam:   "parameter",
		ObjField:   "field",
		ObjFixture: "variable",
	}
)

func (b *body) setType(x ast.Expr, t types.Type) { b.c.info.Types[x] = t }

func (b *body) typeOf(x ast.Expr) types.Type {
	if t, ok := b.c.info.Types[x]; ok {
		return t
	}
	return invalid
}

// expr computes the type of an expression; want is the expected type or
// nil. The expected type sets the type of literals, enum values and []
// (plan, §11.2) but does not check compatibility: check does.
func (b *body) expr(x ast.Expr, want types.Type) types.Type {
	t := b.expr0(x, want)
	b.setType(x, t)
	return t
}

// check checks an expression against the expected type.
func (b *body) check(x ast.Expr, want types.Type) types.Type {
	if want == nil {
		return b.expr(x, nil)
	}
	t := b.expr(x, want)
	if !assignable(t, want) {
		b.mismatch(x, want, t)
	}
	return t
}

func (b *body) mismatch(x ast.Expr, want, got types.Type) {
	if types.IsInvalid(got) || types.IsInvalid(want) {
		return
	}
	if isOptional(got) && assignable(types.Unwrap(got), want) {
		b.c.bag.Add(diag.E0219, rangeOf(x), b.c.quote(x))
		return
	}
	b.c.bag.Add(diag.E0202, rangeOf(x), want.String(), got.String())
}

func (b *body) expr0(x ast.Expr, want types.Type) types.Type {
	switch x := x.(type) {
	case *ast.Ident:
		return b.ident(x, want)
	case *ast.BasicLit:
		return literal(x, want)
	case *ast.InterpString:
		for _, p := range x.Parts {
			if p.X != nil {
				b.expr(p.X, nil)
			}
		}
		return tString
	case *ast.ParenExpr:
		return b.expr(x.X, want)
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			b.cond(x)
			return tBool
		}
		t := b.expr(x.X, numericWant(want))
		switch {
		case types.IsInvalid(t):
		case isOptional(t):
			b.c.bag.Add(diag.E0219, rangeOf(x.X), b.c.quote(x.X))
			return invalid
		case !isNumeric(t) && !isBasic(t, types.Duration):
			b.c.bag.Add(diag.E0220, rangeOf(x), "-", t.String())
			return invalid
		}
		return t
	case *ast.BinaryExpr:
		return b.binary(x, want)
	case *ast.IsNoneExpr:
		t := b.expr(x.X, nil)
		if !types.IsInvalid(t) && !isOptional(t) {
			op := "is none"
			if x.Not {
				op = "is not none"
			}
			b.c.bag.Add(diag.E0220, rangeOf(x), op, t.String())
		}
		return tBool
	case *ast.InExpr:
		return b.in(x)
	case *ast.SelectorExpr:
		return b.selector(x)
	case *ast.CallExpr:
		return b.call(x, want, false)
	case *ast.BracketExpr:
		return b.bracket(x, want)
	case *ast.MapLit:
		return b.mapLit(x, want)
	case *ast.SearchExpr:
		return b.search(x)
	case *ast.NewIDExpr:
		return b.newID(x, want)
	case *ast.AllExpr:
		return b.all(x)
	}
	return invalid
}

func numericWant(want types.Type) types.Type {
	if w := types.Unwrap(want); want != nil && (isNumeric(w) || isBasic(w, types.Duration)) {
		return w
	}
	return nil
}

func literal(x *ast.BasicLit, want types.Type) types.Type {
	var w types.Type
	if want != nil {
		w = types.Unwrap(want)
	}
	switch x.Kind {
	case token.INT:
		if w != nil && isNumeric(w) {
			return w
		}
		return tInt
	case token.DECIMAL:
		if w != nil && isBasic(w, types.Float) {
			return w
		}
		return tDecimal
	case token.STRING:
		return tString
	case token.DURATION:
		return tDuration
	case token.DATE:
		return tDate
	case token.TIMESTAMP:
		return tTimestamp
	case token.TRUE, token.FALSE:
		return tBool
	case token.NONE:
		if isOptional(want) {
			return want
		}
		return tNone
	}
	return invalid
}

// ident resolves a name in an expression.
func (b *body) ident(id *ast.Ident, want types.Type) types.Type {
	enum, _ := types.Unwrap(want).(*types.Enum)
	if want == nil {
		enum = nil
	}
	if o := b.lookup(id); o != nil {
		if enum != nil && enum.Decl.Value(id.Name) != nil {
			if kind, ok := kindGenitive[o.Kind]; ok {
				b.c.bag.Add(diag.E0105, rangeOf(id), id.Name, kind, enum.Decl.Name, enum.Decl.Name, id.Name)
				b.c.info.Uses[id] = o
				return invalid
			}
		}
		b.c.info.Uses[id] = o
		return b.narrowed(id, o.Type)
	}
	if enum != nil {
		if v := enum.Decl.Value(id.Name); v != nil {
			b.c.info.Uses[id] = &Object{Kind: ObjEnumValue, Name: id.Name, Type: enum, Value: v, Enum: enum.Decl}
			return enum
		}
	}
	if d := b.c.lookup(id, id.Name); d != nil {
		b.c.bag.Add(diag.E0214, rangeOf(id), diag.Message{Format: textTypeNotValue, Args: []any{id.Name}})
		return invalid
	}
	b.unknown(id)
	return invalid
}

// unknown reports an unknown name with a hint.
func (b *body) unknown(id *ast.Ident) {
	d := b.c.bag.Add(diag.E0201, rangeOf(id), id.Name)
	for _, td := range b.c.info.DeclList {
		if v := td.Value(id.Name); v != nil {
			d.Hint(hintEnumValue, td.Name, td.Name, id.Name)
			return
		}
	}
	var cands []string
	for _, sc := range b.scopes {
		for name := range sc {
			cands = append(cands, name)
		}
	}
	if b.fieldsVisible() {
		for _, f := range b.owner.Fields {
			cands = append(cands, f.Name)
		}
	}
	if b.record != nil {
		for _, f := range b.record.Fields {
			cands = append(cands, f.Name)
		}
	}
	if s := closest(id.Name, cands); s != "" {
		d.Hint(hintDidYouMean, s)
	}
}

// isTypeRef reports whether the name names a declaring type (not a value).
func (b *body) isTypeRef(id *ast.Ident) *types.Decl {
	d := b.c.lookup(id, id.Name)
	if d == nil || b.lookupQuiet(id.Name) != nil || b.fieldNamed(id.Name) {
		return nil
	}
	return d
}

// fieldNamed reports whether the name names a field of the record or of its own type.
func (b *body) fieldNamed(name string) bool {
	if b.record != nil && recordField(b.record, name) != nil {
		return true
	}
	return b.fieldsVisible() && b.owner.Field(name) != nil
}

func (b *body) binary(x *ast.BinaryExpr, want types.Type) types.Type {
	switch x.Op {
	case token.AND, token.OR:
		b.cond(x)
		return tBool
	case token.EQ, token.NE:
		tx, ty := b.operands(x.X, x.Y, nil)
		if !comparable(tx, ty) {
			b.c.bag.Add(diag.E0220, rangeOf(x), x.Op.String(), pair(tx, ty))
		}
		return tBool
	case token.LT, token.LE, token.GT, token.GE:
		tx, ty := b.operands(x.X, x.Y, nil)
		if types.IsInvalid(tx) || types.IsInvalid(ty) {
			return tBool
		}
		if b.optionalOperand(x.X, tx) || b.optionalOperand(x.Y, ty) {
			return tBool
		}
		if !identical(tx, ty) || !ordered(tx) {
			b.c.bag.Add(diag.E0220, rangeOf(x), x.Op.String(), pair(tx, ty))
		}
		return tBool
	case token.COALESCE:
		var hint types.Type
		if want != nil {
			hint = optional(types.Unwrap(want))
		}
		tx := b.expr(x.X, hint)
		if types.IsInvalid(tx) {
			b.expr(x.Y, nil)
			return invalid
		}
		if !isOptional(tx) {
			b.c.bag.Add(diag.E0220, rangeOf(x), "??", tx.String())
			b.expr(x.Y, nil)
			return invalid
		}
		elem := types.Unwrap(tx)
		ty := b.expr(x.Y, elem)
		if !assignable(ty, tx) {
			b.mismatch(x.Y, elem, ty)
		}
		if isOptional(ty) {
			return tx
		}
		return elem
	}
	// Arithmetic.
	tx, ty := b.operands(x.X, x.Y, numericWant(want))
	if types.IsInvalid(tx) || types.IsInvalid(ty) {
		return invalid
	}
	if b.optionalOperand(x.X, tx) || b.optionalOperand(x.Y, ty) {
		return invalid
	}
	if t := arith(x.Op, tx, ty); t != nil {
		return t
	}
	b.c.bag.Add(diag.E0220, rangeOf(x), x.Op.String(), pair(tx, ty))
	return invalid
}

func pair(a, b types.Type) string {
	if identical(a, b) {
		return a.String()
	}
	return a.String() + " and " + b.String()
}

func (b *body) optionalOperand(x ast.Expr, t types.Type) bool {
	if isOptional(t) {
		b.c.bag.Add(diag.E0219, rangeOf(x), b.c.quote(x))
		return true
	}
	return false
}

// operands computes the operand types: a literal gets the type of the other operand.
func (b *body) operands(x, y ast.Expr, hint types.Type) (types.Type, types.Type) {
	if isUntyped(x) && !isUntyped(y) {
		ty := b.expr(y, hint)
		return b.expr(x, operandWant(x, ty)), ty
	}
	tx := b.expr(x, hint)
	if isBasic(tx, types.Timestamp) || isBasic(tx, types.Date) {
		if !isUntyped(y) {
			return tx, b.expr(y, nil)
		}
	}
	return tx, b.expr(y, operandWant(y, tx))
}

// operandWant is the expected type of the second operand: optional for
// none, without "?" otherwise (enum values, numbers).
func operandWant(x ast.Expr, other types.Type) types.Type {
	if l, ok := x.(*ast.BasicLit); ok && l.Kind == token.NONE {
		return optional(other)
	}
	return types.Unwrap(other)
}

// arith returns the result type of arithmetic or nil.
func arith(op token.Kind, a, b types.Type) types.Type {
	switch {
	case isNumeric(a) && identical(a, b):
		return a
	case op == token.ADD && isBasic(a, types.String) && isBasic(b, types.String):
		return a
	case (op == token.ADD || op == token.SUB) && isBasic(a, types.Duration) && isBasic(b, types.Duration):
		return a
	case op == token.MUL && isBasic(a, types.Duration) && (isBasic(b, types.Int) || isBasic(b, types.Bigint)):
		return a
	case op == token.MUL && isBasic(b, types.Duration) && (isBasic(a, types.Int) || isBasic(a, types.Bigint)):
		return b
	case (op == token.ADD || op == token.SUB) && isBasic(a, types.Timestamp) && isBasic(b, types.Duration):
		return a
	case op == token.SUB && isBasic(a, types.Timestamp) && isBasic(b, types.Timestamp):
		return tDuration
	}
	return nil
}

func (b *body) in(x *ast.InExpr) types.Type {
	tx := b.expr(x.X, nil)
	ty := b.expr(x.Y, &types.List{Elem: types.Unwrap(tx)})
	if types.IsInvalid(tx) || types.IsInvalid(ty) {
		return tBool
	}
	elem := elemOf(ty)
	if m, ok := ty.(*types.Map); ok {
		elem = m.Key
	}
	op := "in"
	if x.Not {
		op = "not in"
	}
	if elem == nil || !comparable(tx, elem) {
		b.c.bag.Add(diag.E0220, rangeOf(x), op, pair(tx, ty))
	}
	return tBool
}

// quote returns the text of an expression in quotes for a message.
func (c *checker) quote(x ast.Expr) string {
	s := "…"
	for _, f := range c.files {
		if f.Contains(x.Pos()) && f.Contains(x.End()) {
			s = string(f.Source()[f.Offset(x.Pos()):f.Offset(x.End())])
			break
		}
	}
	return "'" + s + "'"
}

// selector is x.name or x?.name without a call.
func (b *body) selector(x *ast.SelectorExpr) types.Type {
	if o := b.featureSelector(x); o != nil {
		return o.Type
	}
	var d *types.Decl
	if id, ok := x.X.(*ast.Ident); ok && !b.c.isNamespace(id, id.Name) {
		d = b.isTypeRef(id)
	} else if p := path(x.X); len(p) > 1 && b.lookupQuiet(p[0].Name) == nil && !b.fieldNamed(p[0].Name) {
		d = b.c.lookup(x.X, names(p)...) // Ordering.OrderStatus.searching
	}
	if d != nil {
		if d.Kind == types.EnumDecl {
			if v := d.Value(x.Sel.Name); v != nil {
				t := &types.Enum{Decl: d}
				b.c.info.Uses[x.Sel] = &Object{Kind: ObjEnumValue, Name: v.Name, Type: t, Value: v, Enum: d}
				return t
			}
			b.c.bag.Add(diag.E0221, rangeOf(x.Sel), d.Name, diag.Message{Format: textNotEnumValue, Args: []any{x.Sel.Name}})
			return invalid
		}
		b.c.bag.Add(diag.E0214, rangeOf(x), diag.Message{Format: textTypeNotValue, Args: []any{names2(path(x))}})
		return invalid
	}
	tx := b.expr(x.X, nil)
	recv, ok := b.receiver(x, tx)
	if !ok {
		return invalid
	}
	t := b.property(x, recv)
	if t == nil {
		return invalid
	}
	// An extra ?. on a value that is always present (E0207) does not make the
	// result optional: otherwise E0219 errors would follow.
	if x.Safe && isOptional(tx) {
		t = optional(t)
	}
	return b.narrowed(x, t)
}

// receiver returns the type of the receiver of x.name: without "?" for ?..
// If the receiver is optional and there is no "?.", it reports E0219.
func (b *body) receiver(x *ast.SelectorExpr, tx types.Type) (types.Type, bool) {
	if types.IsInvalid(tx) {
		return nil, false
	}
	if isOptional(tx) {
		if !x.Safe {
			b.c.bag.Add(diag.E0219, rangeOf(x.X), b.c.quote(x.X))
			return nil, false
		}
		return types.Unwrap(tx), true
	}
	if x.Safe {
		b.c.bag.Add(diag.E0207, rangeOf(x.X), b.c.quote(x.X))
	}
	return tx, true
}

// property returns the type of a field or a built-in property; nil means the error is reported.
func (b *body) property(x *ast.SelectorExpr, recv types.Type) types.Type {
	name := x.Sel.Name
	switch r := recv.(type) {
	case *types.Value:
		if f := r.Decl.Field(name); f != nil {
			return f.Type
		}
	case *types.Record:
		if o := recordField(r.Owner, name); o != nil {
			return o.Type
		}
	case *types.Ref:
		b.c.bag.Add(diag.E0217, rangeOf(x), r.Owner.Name)
		return nil
	default:
		if t := builtinProperty(recv, name); t != nil {
			return t
		}
	}
	b.c.bag.Add(diag.E0221, rangeOf(x.Sel), recv.String(), diag.Message{Format: textNoField, Args: []any{name}})
	return nil
}

// bracket is [a, b, …]: an entity access, a list or a set literal (plan,
// §2, item 1).
func (b *body) bracket(x *ast.BracketExpr, want types.Type) types.Type {
	w := types.Unwrap(want)
	if want != nil {
		if elem := elemOf(w); elem != nil {
			if _, page := w.(*types.Page); !page {
				for _, e := range x.Elems {
					b.check(e, elem)
				}
				return w
			}
		}
	}
	switch len(x.Elems) {
	case 0:
		b.c.bag.Add(diag.E0218, rangeOf(x), textEmptyList)
		return invalid
	case 1:
		if id, ok := x.Elems[0].(*ast.Ident); ok {
			if d := b.isTypeRef(id); d != nil && d.Kind.HasID() {
				if d.Owner != nil {
					// [Stop]: the records of an inner entity of its own aggregate.
					if !b.innerHere(x, d) {
						return invalid
					}
					return &types.List{Elem: &types.Record{Owner: d}}
				}
				// [Order] without sections: all entities of the type; restrictions are stage 7.
				b.notInFake(x, fakeRef)
				if !b.c.visible(d, x) {
					b.c.closed(id, d.Name, d.Context)
					return invalid
				}
				return &types.List{Elem: &types.Record{Owner: d}}
			}
		}
		t := b.expr(x.Elems[0], nil)
		if id, ok := types.Unwrap(t).(*types.ID); ok {
			b.notInFake(x, fakeRef)
			// [x] reads an entity of its own bounded context: an ACL does not
			// read the source (docs/plans/0011, §4).
			owner := id.Owner
			if b.kind != ctxTest && !b.c.visible(owner, x) {
				b.c.closed(x, owner.Name, owner.Context)
				return invalid
			}
			if owner.Owner != nil {
				// [stop]: a record of an inner entity of its own aggregate.
				if !b.innerHere(x, owner) {
					return invalid
				}
				return &types.Optional{Elem: &types.Record{Owner: owner}}
			}
			if b.kind == ctxQuery {
				return &types.Optional{Elem: &types.Record{Owner: owner}}
			}
			return &types.Optional{Elem: &types.Ref{Owner: owner}}
		}
		if types.IsInvalid(t) {
			return invalid
		}
		return &types.List{Elem: t}
	}
	t := b.expr(x.Elems[0], nil)
	for _, e := range x.Elems[1:] {
		b.check(e, t)
	}
	if types.IsInvalid(t) {
		return invalid
	}
	return &types.List{Elem: t}
}

func (b *body) mapLit(x *ast.MapLit, want types.Type) types.Type {
	if m, ok := types.Unwrap(want).(*types.Map); ok && want != nil {
		for _, e := range x.Entries {
			b.check(e.Key, m.Key)
			b.check(e.Value, m.Elem)
		}
		return m
	}
	if len(x.Entries) == 0 {
		b.c.bag.Add(diag.E0218, rangeOf(x), textEmptyMap)
		return invalid
	}
	k := b.expr(x.Entries[0].Key, nil)
	v := b.expr(x.Entries[0].Value, nil)
	for _, e := range x.Entries[1:] {
		b.check(e.Key, k)
		b.check(e.Value, v)
	}
	if types.IsInvalid(k) || types.IsInvalid(v) {
		return invalid
	}
	return &types.Map{Key: k, Elem: v}
}

// search is [Type] with sections; in where, sort and select bare names are record fields.
func (b *body) search(x *ast.SearchExpr) types.Type {
	if b.kind != ctxQuery {
		b.c.bag.Add(diag.E0303, rangeOf(x), ctxNames[b.kind], textSearchOnly)
	}
	d := b.c.lookup(x.Type, x.Type.Name)
	if d != nil && d.Kind.HasID() && !b.c.visible(d, x) {
		b.c.closed(x.Type, d.Name, d.Context)
		return invalid
	}
	if d == nil || !d.Kind.HasID() {
		if d == nil {
			b.c.unknownType(x.Type, x.Type.Name)
		} else {
			b.c.bag.Add(diag.E0214, rangeOf(x.Type), diag.Message{Format: textTypeNotValue, Args: []any{d.Name}})
		}
		return invalid
	}
	b.c.info.TypeRefs[x] = d
	saved := b.record
	b.record = d
	if x.Where != nil {
		b.cond(x.Where)
	}
	for _, k := range x.Sort {
		b.sortKey(d, k)
	}
	if x.Page != nil {
		b.check(x.Page, &types.PageReq{})
	}
	if x.Limit != nil {
		b.check(x.Limit, tInt)
	}
	var elem types.Type = &types.Record{Owner: d}
	if x.Select != nil {
		elem = b.expr(x.Select, nil)
	}
	b.record = saved
	if types.IsInvalid(elem) {
		return invalid
	}
	if x.Page != nil {
		return &types.Page{Elem: elem}
	}
	return &types.List{Elem: elem}
}

// sortKey checks that the path of a sort key goes through record fields.
func (b *body) sortKey(d *types.Decl, k *ast.SortKey) {
	var t types.Type = &types.Record{Owner: d}
	for _, e := range k.Path.Elems {
		var next types.Type
		switch r := types.Unwrap(t).(type) {
		case *types.Record:
			if o := recordField(r.Owner, e.Name); o != nil {
				next = o.Type
			}
		case *types.Value:
			if f := r.Decl.Field(e.Name); f != nil {
				next = f.Type
			}
		}
		if next == nil {
			b.c.bag.Add(diag.E0221, rangeOf(e), t.String(), diag.Message{Format: textNoField, Args: []any{e.Name}})
			return
		}
		t = next
	}
}

func (b *body) newID(x *ast.NewIDExpr, want types.Type) types.Type {
	switch b.kind {
	case ctxCommand, ctxOn, ctxMigration, ctxTest, ctxUI:
	default:
		b.c.bag.Add(diag.E0304, rangeOf(x), ctxNames[b.kind], "new", textNewWhy)
		return invalid
	}
	var id *types.ID
	if x.Type == nil {
		// A bare new in a layout argument: the type comes from the parameter.
		var ok bool
		if id, ok = types.Unwrap(want).(*types.ID); !ok {
			if want != nil && !types.IsInvalid(want) {
				b.c.bag.Add(diag.E0706, rangeOf(x), diag.Message{Format: textUINewNotID, Args: []any{want.String()}})
			}
			return invalid
		}
	} else {
		var ok bool
		if id, ok = b.c.resolveType(x.Type).(*types.ID); !ok {
			return invalid
		}
	}
	if !isBasic(id.Base, types.UUID) && !isBasic(id.Base, types.Bigint) {
		strategy := id.Base.String()
		b.c.bag.Add(diag.E0215, rangeOf(x), id.Owner.Name, id.Owner.Name, strategy)
	}
	return id
}

func (b *body) all(x *ast.AllExpr) types.Type {
	if b.kind != ctxMigration {
		b.c.bag.Add(diag.E0303, rangeOf(x), ctxNames[b.kind], textAllOnly)
	}
	d := b.c.lookup(x.Type, x.Type.Name)
	if d == nil || d.ID == nil || d.Kind == types.ProjectionDecl {
		b.c.unknownType(x.Type, x.Type.Name)
		return invalid
	}
	b.c.info.TypeRefs[x] = d
	return &types.List{Elem: d.ID}
}
