package sema

import "github.com/tycheg/reasonr-compiler/internal/types"

// identical reports whether the types are the same.
func identical(a, b types.Type) bool {
	switch a := a.(type) {
	case *types.Basic:
		b, ok := b.(*types.Basic)
		return ok && a.Kind == b.Kind
	case *types.Optional:
		b, ok := b.(*types.Optional)
		return ok && identical(a.Elem, b.Elem)
	case *types.List:
		b, ok := b.(*types.List)
		return ok && identical(a.Elem, b.Elem)
	case *types.Set:
		b, ok := b.(*types.Set)
		return ok && identical(a.Elem, b.Elem)
	case *types.Map:
		b, ok := b.(*types.Map)
		return ok && identical(a.Key, b.Key) && identical(a.Elem, b.Elem)
	case *types.Page:
		b, ok := b.(*types.Page)
		return ok && identical(a.Elem, b.Elem)
	case *types.Enum:
		b, ok := b.(*types.Enum)
		return ok && a.Decl == b.Decl
	case *types.Value:
		b, ok := b.(*types.Value)
		return ok && a.Decl == b.Decl
	case *types.ID:
		b, ok := b.(*types.ID)
		return ok && a.Owner == b.Owner
	case *types.Ref:
		b, ok := b.(*types.Ref)
		return ok && a.Owner == b.Owner
	case *types.Record:
		b, ok := b.(*types.Record)
		return ok && a.Owner == b.Owner
	case *types.TimerID:
		_, ok := b.(*types.TimerID)
		return ok
	case *types.Caller:
		_, ok := b.(*types.Caller)
		return ok
	case *types.PageReq:
		_, ok := b.(*types.PageReq)
		return ok
	case *types.Void:
		_, ok := b.(*types.Void)
		return ok
	case *types.None:
		_, ok := b.(*types.None)
		return ok
	}
	return false
}

// assignable reports whether a value of type from may be passed where to is
// expected: the same type or a lift T → T?, none → T?.
func assignable(from, to types.Type) bool {
	if types.IsInvalid(from) || types.IsInvalid(to) || identical(from, to) {
		return true
	}
	if o, ok := to.(*types.Optional); ok {
		if _, none := from.(*types.None); none {
			return true
		}
		return assignable(types.Unwrap(from), o.Elem)
	}
	return false
}

// comparable reports whether values can be compared for equality: the types
// match up to optionality.
func comparable(a, b types.Type) bool {
	if types.IsInvalid(a) || types.IsInvalid(b) {
		return true
	}
	_, an := a.(*types.None)
	_, bn := b.(*types.None)
	switch {
	case an && bn:
		return false
	case an:
		_, ok := b.(*types.Optional)
		return ok
	case bn:
		_, ok := a.(*types.Optional)
		return ok
	}
	return identical(types.Unwrap(a), types.Unwrap(b))
}

func isNumeric(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && (b.Kind == types.Int || b.Kind == types.Bigint || b.Kind == types.Float || b.Kind == types.Decimal)
}

func isBasic(t types.Type, k types.BasicKind) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Kind == k
}

// ordered reports whether the type has an order (<, >).
func ordered(t types.Type) bool {
	b, ok := t.(*types.Basic)
	if !ok {
		return false
	}
	switch b.Kind {
	case types.Int, types.Bigint, types.Float, types.Decimal, types.String,
		types.Date, types.Time, types.Timestamp, types.Duration:
		return true
	}
	return false
}

func isOptional(t types.Type) bool {
	_, ok := t.(*types.Optional)
	return ok
}

// optional returns T? (the type itself if it is already optional).
func optional(t types.Type) types.Type {
	if isOptional(t) || types.IsInvalid(t) {
		return t
	}
	return &types.Optional{Elem: t}
}

// elemOf returns the element type of a collection or nil.
func elemOf(t types.Type) types.Type {
	switch t := t.(type) {
	case *types.List:
		return t.Elem
	case *types.Set:
		return t.Elem
	case *types.Page:
		return t.Elem
	}
	return nil
}
