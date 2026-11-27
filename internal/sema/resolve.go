package sema

import (
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// generics are the built-in generic types and the number of their arguments.
var generics = map[string]int{"list": 1, "set": 1, "page": 1, "map": 2}

var (
	noteNotType    = "%q is declared here, but it is %s, not a value type"
	noteNoID       = "%q is declared here: only entities, actors and projections have ids"
	hintSequence   = "sequence is an id strategy and is only allowed in the id line"
	hintDidYouMean = "did you mean %q?"
	textNotGeneric = "type %q takes no type arguments"
	declKindText   = map[types.DeclKind]string{
		types.ExternalDecl:  "an external system",
		types.AppDecl:       "an app",
		types.ScreenDecl:    "a screen",
		types.PrimitiveDecl: "a UI primitive",
	}
)

// genericArity is the message about a wrong number of arguments of a generic type.
func genericArity(name string) string {
	switch name {
	case "map":
		return "map takes two type arguments: map<K, V>"
	default:
		return name + " takes one type argument: " + name + "<T>"
	}
}

// resolveType resolves a type expression and records the result in Info.Types.
func (c *checker) resolveType(t ast.TypeExpr) types.Type {
	if rt, ok := c.info.TypeExprs[t]; ok {
		return rt
	}
	rt := c.resolve(t)
	c.info.TypeExprs[t] = rt
	return rt
}

func (c *checker) resolve(t ast.TypeExpr) types.Type {
	switch t := t.(type) {
	case *ast.OptionalType:
		elem := c.resolveType(t.Elem)
		if types.IsInvalid(elem) {
			return invalid
		}
		return &types.Optional{Elem: elem}
	case *ast.GenericType:
		name := t.Name.Name
		n, ok := generics[name]
		if !ok {
			if c.isTypeName(t.Name, name) {
				c.bag.Add(diag.E0109, rangeOf(t), diag.Message{Format: textNotGeneric, Args: []any{name}})
			} else {
				c.unknownType(t.Name, name)
			}
			return invalid
		}
		args := make([]types.Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = c.resolveType(a)
		}
		if len(args) != n {
			c.bag.Add(diag.E0109, rangeOf(t), genericArity(name))
			return invalid
		}
		for _, a := range args {
			if types.IsInvalid(a) {
				return invalid
			}
		}
		switch name {
		case "list":
			return &types.List{Elem: args[0]}
		case "set":
			return &types.Set{Elem: args[0]}
		case "page":
			return &types.Page{Elem: args[0]}
		}
		return &types.Map{Key: args[0], Elem: args[1]}
	case *ast.NamedType:
		return c.named(t)
	}
	return invalid
}

func (c *checker) named(t *ast.NamedType) types.Type {
	full := t.String()
	if len(t.Path) == 2 && t.Path[0].Name == "Timer" && t.Path[1].Name == "Id" {
		return &types.TimerID{}
	}
	if len(t.Path) == 1 {
		name := t.Path[0].Name
		if b := types.Basics[name]; b != nil {
			return b
		}
		if name == "PageRequest" {
			return &types.PageReq{}
		}
		if _, ok := generics[name]; ok {
			c.bag.Add(diag.E0109, rangeOf(t), genericArity(name))
			return invalid
		}
	}
	d, n := c.lookupPrefix(t, names(t.Path))
	switch {
	case d != nil && n == len(t.Path)-1 && t.Path[n].Name == "Id":
		if d.ID != nil {
			return d.ID
		}
		c.bag.Add(diag.E0107, rangeOf(t), full).Note(rangeOf(declIdent(d.Node)), noteNoID, d.Short)
		return invalid
	case d == nil || n != len(t.Path):
		if full == "sequence" {
			c.bag.Add(diag.E0107, rangeOf(t), full).Hint(hintSequence)
			return invalid
		}
		c.unknownType(t, full)
		return invalid
	}
	switch d.Kind {
	case types.ValueDecl:
		return &types.Value{Decl: d}
	case types.EnumDecl:
		return &types.Enum{Decl: d}
	case types.EntityDecl, types.ActorDecl, types.ProjectionDecl:
		return &types.Record{Owner: d}
	}
	c.bag.Add(diag.E0107, rangeOf(t), full).
		Note(rangeOf(declIdent(d.Node)), noteNotType, full, declKindText[d.Kind])
	return invalid
}

// isTypeName reports whether the name names a value type.
func (c *checker) isTypeName(at ast.Node, name string) bool {
	if types.Basics[name] != nil || name == "PageRequest" {
		return true
	}
	d := c.lookup(at, name)
	return d != nil && d.Kind <= types.EnumDecl
}

func (c *checker) unknownType(n ast.Node, name string) {
	d := c.bag.Add(diag.E0107, rangeOf(n), name)
	var cands []string
	for b := range types.Basics {
		cands = append(cands, b)
	}
	cands = append(cands, "PageRequest", "Timer.Id")
	for g := range generics {
		cands = append(cands, g)
	}
	for _, td := range c.info.DeclList {
		if td.Kind <= types.EnumDecl {
			cands = append(cands, td.Name)
		}
		if td.ID != nil {
			cands = append(cands, td.Name+".Id")
		}
	}
	if s := closest(name, cands); s != "" {
		d.Hint(hintDidYouMean, s)
	}
}

func declIdent(d ast.Decl) ast.Node {
	if _, name, ok := declKind(d); ok {
		return name
	}
	return d
}

// builtins are the built-in values (RFC §6.4, §8.2): they cannot be redeclared.
var builtins = map[string]bool{
	"id": true, "now": true, "today": true, "after": true, "hasErrors": true,
	"me": true, "caller": true, "self": true,
}

func (c *checker) builtinName(id *ast.Ident) bool {
	if builtins[id.Name] {
		c.bag.Add(diag.E0108, rangeOf(id), id.Name)
		return true
	}
	return false
}

// closest returns the most similar name from cands (at a distance of at
// most a quarter of the length plus one), or "" if none is similar.
func closest(name string, cands []string) string {
	sort.Strings(cands)
	best, bestDist := "", len([]rune(name))/4+2
	for _, cand := range cands {
		if cand == name {
			continue
		}
		if d := distance(strings.ToLower(name), strings.ToLower(cand)); d < bestDist {
			best, bestDist = cand, d
		}
	}
	return best
}

// distance is the Levenshtein distance in runes.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
