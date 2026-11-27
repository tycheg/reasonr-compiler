package sema

import (
	"sort"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Declaration indexes for lookups without a pass over all declarations of
// the model (docs/plans/0015: at 10 million lines a pass per name is
// quadratic). An index is built on the first lookup and rebuilt if
// declarations were added since (len(DeclList) changed).

// declIndex holds declarations by short name.
type declIndex struct {
	n     int
	short map[string][]*types.Decl
}

func (x *declIndex) of(list []*types.Decl) map[string][]*types.Decl {
	if x.short == nil || x.n != len(list) {
		x.n, x.short = len(list), make(map[string][]*types.Decl, len(list))
		for _, d := range list {
			x.short[d.Short] = append(x.short[d.Short], d)
		}
	}
	return x.short
}

// uniqueShort looks up a short name in the whole project: there must be one
// top-level declaration with that name.
func (i *Info) uniqueShort(name string) *types.Decl {
	var found *types.Decl
	for _, d := range i.short.of(i.DeclList)[name] {
		if d.Owner != nil {
			continue
		}
		if found != nil {
			return nil
		}
		found = d
	}
	return found
}

// methodIndex holds the methods of all declarations by name, in declaration order.
type methodIndex struct {
	n      int
	byName map[string][]*types.Method
	owner  map[*types.Method]*types.Decl
}

func (c *checker) methodsNamed(name string) ([]*types.Method, map[*types.Method]*types.Decl) {
	x := &c.methods
	if x.byName == nil || x.n != len(c.info.DeclList) {
		x.n, x.byName, x.owner = len(c.info.DeclList), map[string][]*types.Method{}, map[*types.Method]*types.Decl{}
		for _, d := range c.info.DeclList {
			for _, m := range d.Methods {
				if d.Method(m.Name) == m { // like d.Method: repeated names do not count
					x.byName[m.Name] = append(x.byName[m.Name], m)
					x.owner[m] = d
				}
			}
		}
	}
	return x.byName[name], x.owner
}

// span is the source place of a part of an aggregate or an actor.
type span struct {
	start, end source.Pos
	decl       *types.Decl
}

// aggIndex holds the parts of aggregates and actors by start.
type aggIndex struct {
	n     int
	spans []span
}

// aggOf returns the aggregate (or actor) whose declaration holds node n;
// for an inner entity, its aggregate.
func (c *checker) aggOf(n ast.Node) *types.Decl {
	if n == nil {
		return nil
	}
	x := &c.aggs
	if x.spans == nil || x.n != len(c.info.DeclList) {
		x.n, x.spans = len(c.info.DeclList), x.spans[:0]
		for _, d := range c.info.DeclList {
			if d.Kind != types.EntityDecl && d.Kind != types.ActorDecl {
				continue
			}
			for _, part := range c.prog.Parts(d.Node) { // a partial aggregate, by file
				x.spans = append(x.spans, span{part.Pos(), part.End(), d})
			}
		}
		sort.SliceStable(x.spans, func(i, j int) bool { return x.spans[i].start < x.spans[j].start })
	}
	pos := n.Pos()
	i := sort.Search(len(x.spans), func(i int) bool { return x.spans[i].start > pos }) - 1
	// Parts do not nest, but a copy of a partial aggregate starts where its
	// first part does: search backwards among those starting no later.
	for ; i >= 0; i-- {
		if s := x.spans[i]; pos < s.end {
			return s.decl.Root()
		}
		if i > 0 && x.spans[i-1].start != x.spans[i].start {
			break
		}
	}
	return nil
}
