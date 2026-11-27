package sema

import (
	"fmt"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/token"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// facts are the narrowed types of expressions: expression key → type
// without "?" (plan, §11.5).
type facts map[string]types.Type

func (f facts) clone() facts {
	out := make(facts, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out
}

// with returns a copy of f with the facts of g added.
func (f facts) with(g facts) facts {
	out := f.clone()
	for k, v := range g {
		out[k] = v
	}
	return out
}

// intersect keeps the facts that hold in both sets.
func intersect(a, b facts) facts {
	out := facts{}
	for k, v := range a {
		if w, ok := b[k]; ok && identical(v, w) {
			out[k] = v
		}
	}
	return out
}

// union merges fact sets.
func union(a, b facts) facts { return a.with(b) }

// forget drops facts about expressions starting with key k.
func (f facts) forget(k string) {
	for key := range f {
		if key == k || len(key) > len(k) && key[:len(k)] == k && key[len(k)] == '.' {
			delete(f, key)
		}
	}
}

// key returns the key of a narrowable expression: the name of a variable,
// parameter or field and the chain of fields after it. An empty key means
// the expression is not narrowed.
func (b *body) key(x ast.Expr) string {
	switch x := x.(type) {
	case *ast.ParenExpr:
		return b.key(x.X)
	case *ast.Ident:
		o := b.c.info.Uses[x]
		if o == nil {
			return ""
		}
		switch o.Kind {
		case ObjLocal, ObjParam, ObjField, ObjFixture:
			return keyOf(o)
		}
	case *ast.SelectorExpr:
		if base := b.key(x.X); base != "" {
			return base + "." + x.Sel.Name
		}
	}
	return ""
}

func nodeKey(n ast.Node) string { return fmt.Sprintf("%p", n) }

// narrowed returns the type of an expression taking the facts into account.
func (b *body) narrowed(x ast.Expr, t types.Type) types.Type {
	if k := b.key(x); k != "" {
		if n, ok := b.facts[k]; ok {
			return n
		}
	}
	return t
}

// cond checks a condition and returns the facts that hold when it is true
// and when it is false.
func (b *body) cond(x ast.Expr) (whenTrue, whenFalse facts) {
	switch e := x.(type) {
	case *ast.ParenExpr:
		t, f := b.cond(e.X)
		b.setType(x, tBool)
		return t, f
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			t, f := b.cond(e.X)
			b.setType(x, tBool)
			return f, t
		}
	case *ast.BinaryExpr:
		switch e.Op {
		case token.AND:
			ta, fa := b.cond(e.X)
			saved := b.facts
			b.facts = saved.with(ta)
			tb, fb := b.cond(e.Y)
			b.facts = saved
			b.setType(x, tBool)
			return union(ta, tb), intersect(fa, fb)
		case token.OR:
			ta, fa := b.cond(e.X)
			saved := b.facts
			b.facts = saved.with(fa)
			tb, fb := b.cond(e.Y)
			b.facts = saved
			b.setType(x, tBool)
			return intersect(ta, tb), union(fa, fb)
		}
	case *ast.IsNoneExpr:
		b.expr(x, nil)
		t := b.typeOf(e.X)
		k := b.key(e.X)
		if k == "" || !isOptional(t) {
			return nil, nil
		}
		f := facts{k: types.Unwrap(t)}
		if e.Not {
			return f, nil
		}
		return nil, f
	case *ast.BracketExpr:
		// if [x] is short for [x] is not none.
		if len(e.Elems) == 1 {
			t := b.expr(x, nil)
			if isRefLike(t) {
				b.setType(x, t)
				if k := b.key(e.Elems[0]); k != "" && isOptional(b.typeOf(e.Elems[0])) {
					return facts{k: types.Unwrap(b.typeOf(e.Elems[0]))}, nil
				}
				return nil, nil
			}
			b.mismatch(x, tBool, t)
			return nil, nil
		}
	}
	b.check(x, tBool)
	return nil, nil
}

// isRefLike reports whether a type is the result of an access [x]: Ref? or Record?.
func isRefLike(t types.Type) bool {
	switch types.Unwrap(t).(type) {
	case *types.Ref, *types.Record:
		return isOptional(t)
	}
	return false
}
