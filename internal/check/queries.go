package check

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Queries checks queries and picks indexes for searches (plan, §12.2). The
// result is map[*ast.SearchExpr]*Search: a parsed search without errors.
var Queries = &Analyzer{
	Name:  "queries",
	Doc:   "search only by indexes, page or limit, the result is a value object, reading records of own type only",
	Codes: []diag.Code{"E0501", "E0502", "E0503", "E0504", "E0505", "E0506", "E0509", "E0510", "E0905"},
	Run:   runQueries,
}

var (
	textOr          = "or"
	textNot         = "not"
	textTwoFields   = "a comparison of two fields"
	textNoField     = "a condition without a record field"
	textRanges      = "ranges on several fields at once"
	textAddIndex    = "add %s"
	textAddIndexMsg = "add %s"
)

// Search is a search parsed for lowering into a FindSpec template.
type Search struct {
	Index *types.Index // nil: no conditions or sorting, the engine chooses
	Conds []Cond
}

// Cond is a search condition "path op value".
type Cond struct {
	Path  []string
	Op    token.Kind // EQ, IN, LT, LE, GT, GE
	Value ast.Expr   // nil means "is none"
}

// cond is a search condition "path op value".
type cond struct {
	path  string
	root  *types.Var // the record field the path starts at
	op    token.Kind // EQ, IN, LT, LE, GT, GE
	node  ast.Expr
	value ast.Expr
}

func runQueries(pass *Pass) any {
	chosen := map[*ast.SearchExpr]*Search{}
	for _, m := range bodies(pass.Info) {
		q, ok := m.node.(*ast.QueryDecl)
		if !ok {
			continue
		}
		if md := m.decl.Method(q.Name.Name); md != nil && md.Node == q {
			checkResult(pass, q, md.Result)
		}
		ast.Inspect(m.body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SearchExpr:
				if sr := search(pass, m.decl, x); sr != nil {
					chosen[x] = sr
				}
			case *ast.BracketExpr:
				foreignRecord(pass, m.decl, x)
			}
			return true
		})
	}
	for _, d := range pass.Info.DeclList {
		for _, ix := range d.Indexes {
			personalKeys(pass, d, ix.Node)
		}
	}
	return chosen
}

// checkResult: a query result is a value object, page<V> or list<V> (E0509).
func checkResult(pass *Pass, q *ast.QueryDecl, t types.Type) {
	if types.IsInvalid(t) {
		return
	}
	inner := t
	switch r := t.(type) {
	case *types.Page:
		inner = r.Elem
	case *types.List:
		inner = r.Elem
	}
	if _, ok := inner.(*types.Value); ok {
		return
	}
	if types.IsInvalid(inner) {
		return
	}
	pass.Report(diag.E0509, rangeOf(q.Result), t.String())
}

// foreignRecord: [x] with a record or [Type] of another type in a query
// (E0510), [Type] without search sections (E0505).
func foreignRecord(pass *Pass, owner *types.Decl, x *ast.BracketExpr) {
	switch t := pass.Info.Types[x].(type) {
	case *types.Optional:
		if r, ok := t.Elem.(*types.Record); ok && r.Owner != owner {
			pass.Report(diag.E0510, rangeOf(x), r.Owner.Name, r.Owner.Name)
		}
	case *types.List:
		if r, ok := t.Elem.(*types.Record); ok && len(x.Elems) == 1 {
			if id, isIdent := x.Elems[0].(*ast.Ident); isIdent && id.Name == r.Owner.Name {
				if r.Owner != owner {
					pass.Report(diag.E0510, rangeOf(x), r.Owner.Name, r.Owner.Name)
					return
				}
				pass.Report(diag.E0505, rangeOf(x))
			}
		}
	}
}

func search(pass *Pass, owner *types.Decl, s *ast.SearchExpr) *Search {
	d := pass.Info.TypeRefs[s]
	if d == nil {
		return nil
	}
	if d != owner {
		pass.Report(diag.E0510, rangeOf(s.Type), d.Name, d.Name)
		return nil
	}
	switch {
	case s.Page == nil && s.Limit == nil:
		pass.Report(diag.E0505, rangeOf(s.Type))
	case s.Limit != nil:
		if lit, ok := s.Limit.(*ast.BasicLit); ok && lit.Kind == token.INT {
			if n, err := strconv.Atoi(strings.ReplaceAll(lit.Raw, "_", "")); err == nil && n > 100 {
				pass.Report(diag.E0506, rangeOf(s.Limit), lit.Raw)
			}
		}
	}
	var conds []cond
	ok := true
	if s.Where != nil {
		ok = decompose(pass, s.Where, &conds)
	}
	for _, k := range s.Sort {
		if v := d.Field(k.Path.Elems[0].Name); v != nil && v.Personal {
			pass.Report(diag.E0905, rangeOf(k), k.Path.String())
		}
	}
	if !ok {
		return nil
	}
	sr := &Search{}
	for _, c := range conds {
		sr.Conds = append(sr.Conds, Cond{Path: strings.Split(c.path, "."), Op: c.op, Value: c.value})
	}
	if len(conds) == 0 && len(s.Sort) == 0 {
		return sr
	}
	if sr.Index = matchIndex(pass, d, s, conds); sr.Index == nil {
		return nil
	}
	return sr
}

// decompose splits where into a conjunction of conditions.
func decompose(pass *Pass, x ast.Expr, out *[]cond) bool {
	switch e := x.(type) {
	case *ast.ParenExpr:
		return decompose(pass, e.X, out)
	case *ast.BinaryExpr:
		switch e.Op {
		case token.AND:
			a := decompose(pass, e.X, out)
			b := decompose(pass, e.Y, out)
			return a && b
		case token.OR:
			pass.Report(diag.E0501, rangeOf(e), textOr)
			return false
		case token.EQ, token.NE, token.LT, token.LE, token.GT, token.GE:
			return addCond(pass, e, e.X, e.Op, e.Y, out)
		}
	case *ast.InExpr:
		op := token.IN
		if e.Not {
			op = token.NE
		}
		return addCond(pass, e, e.X, op, e.Y, out)
	case *ast.IsNoneExpr:
		op := token.EQ
		if e.Not {
			op = token.NE
		}
		return addCond(pass, e, e.X, op, nil, out)
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			pass.Report(diag.E0501, rangeOf(e), textNot)
			return false
		}
	}
	pass.Report(diag.E0501, rangeOf(x), textNoField)
	return false
}

var flip = map[token.Kind]token.Kind{token.LT: token.GT, token.LE: token.GE, token.GT: token.LT, token.GE: token.LE}

func addCond(pass *Pass, node, x ast.Expr, op token.Kind, y ast.Expr, out *[]cond) bool {
	lp, lroot := fieldPath(pass.Info, x)
	var rp string
	if y != nil {
		rp, _ = fieldPath(pass.Info, y)
	}
	switch {
	case lp != "" && rp != "":
		pass.Report(diag.E0501, rangeOf(node), textTwoFields)
		return false
	case lp == "" && rp != "":
		if op == token.IN {
			pass.Report(diag.E0501, rangeOf(node), textNoField)
			return false
		}
		x, y = y, x
		lp, lroot = fieldPath(pass.Info, x)
		if f, ok := flip[op]; ok {
			op = f
		}
	case lp == "":
		pass.Report(diag.E0501, rangeOf(node), textNoField)
		return false
	}
	if y != nil {
		if f := recordFieldIn(pass.Info, y); f != nil {
			pass.Report(diag.E0502, rangeOf(f), f.Name)
			return false
		}
	}
	if op == token.NE {
		pass.Report(diag.E0503, rangeOf(node))
		return false
	}
	if lroot != nil && lroot.Personal && op != token.EQ && op != token.IN {
		pass.Report(diag.E0905, rangeOf(node), lp)
		return false
	}
	*out = append(*out, cond{path: lp, root: lroot, op: op, node: node, value: y})
	return true
}

// fieldPath returns a path through record fields (customer, car.carClass), or "".
func fieldPath(info *sema.Info, x ast.Expr) (string, *types.Var) {
	switch e := x.(type) {
	case *ast.ParenExpr:
		return fieldPath(info, e.X)
	case *ast.Ident:
		if o := info.Uses[e]; o != nil && o.Kind == sema.ObjField {
			return e.Name, o.Var
		}
	case *ast.SelectorExpr:
		if p, root := fieldPath(info, e.X); p != "" && !e.Safe {
			return p + "." + e.Sel.Name, root
		}
	}
	return "", nil
}

// recordFieldIn returns the first record field name in an expression, or nil.
func recordFieldIn(info *sema.Info, x ast.Expr) *ast.Ident {
	var found *ast.Ident
	ast.Inspect(x, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && found == nil {
			if o := info.Uses[id]; o != nil && o.Kind == sema.ObjField {
				found = id
			}
		}
		return found == nil
	})
	return found
}

type indexKey struct {
	path string
	desc bool
}

func keysOf(ix *ast.IndexDecl) []indexKey {
	out := make([]indexKey, len(ix.Keys))
	for i, k := range ix.Keys {
		out[i] = indexKey{k.Path.String(), k.Desc()}
	}
	return out
}

// matchIndex picks an index: equalities cover a prefix of the keys, at most
// one range is on the next key, and sorting continues the keys in the same
// or in fully reversed directions.
func matchIndex(pass *Pass, d *types.Decl, s *ast.SearchExpr, conds []cond) *types.Index {
	eq := map[string]bool{}
	var eqOrder []string
	rng := ""
	for _, c := range conds {
		switch c.op {
		case token.EQ, token.IN:
			if !eq[c.path] {
				eq[c.path] = true
				eqOrder = append(eqOrder, c.path)
			}
		default:
			if rng != "" && rng != c.path {
				pass.Report(diag.E0504, rangeOf(s.Where), d.Name, textRanges)
				return nil
			}
			rng = c.path
		}
	}
	var sortKeys []indexKey
	for _, k := range s.Sort {
		sortKeys = append(sortKeys, indexKey{k.Path.String(), k.Desc()})
	}
	var best *types.Index
	bestScore := -1
	for _, ix := range d.Indexes {
		keys := keysOf(ix.Node)
		j := 0
		for j < len(keys) && eq[keys[j].path] {
			j++
		}
		if j != len(eq) {
			continue // not all equalities are in the prefix
		}
		pos := j
		if rng != "" && (pos >= len(keys) || keys[pos].path != rng) {
			continue
		}
		if len(sortKeys) > 0 && !sortMatches(keys[min(pos, len(keys)):], sortKeys) {
			continue
		}
		score := j
		if rng != "" {
			score++
		}
		if score > bestScore {
			best, bestScore = ix, score
		}
	}
	if best != nil {
		return best
	}
	// A hint: an index of the equalities, the range and the sorting.
	var keys []indexKey
	seen := map[string]bool{}
	add := func(k indexKey) {
		if !seen[k.path] {
			seen[k.path] = true
			keys = append(keys, k)
		}
	}
	for _, p := range eqOrder {
		add(indexKey{path: p})
	}
	if rng != "" {
		desc := len(sortKeys) > 0 && sortKeys[0].path == rng && sortKeys[0].desc
		add(indexKey{path: rng, desc: desc})
	}
	for _, k := range sortKeys {
		add(k)
	}
	decl := suggestIndex(keys)
	r := rangeOf(s.Type)
	if s.Where != nil {
		r = rangeOf(s.Where)
	}
	dg := pass.Report(diag.E0504, r, d.Name, diag.Message{Format: textAddIndexMsg, Args: []any{decl}})
	if edit, ok := indexEdit(pass, d, decl); ok {
		dg.Fix(diag.Message{Format: textAddIndex, Args: []any{decl}}, edit)
	}
	return nil
}

func sortMatches(keys, sortKeys []indexKey) bool {
	if len(keys) < len(sortKeys) {
		return false
	}
	same, reversed := true, true
	for i, s := range sortKeys {
		if keys[i].path != s.path {
			return false
		}
		if keys[i].desc != s.desc {
			same = false
		} else {
			reversed = false
		}
	}
	return same || reversed
}

func suggestIndex(keys []indexKey) string {
	var name strings.Builder
	name.WriteString("by")
	parts := make([]string, len(keys))
	for i, k := range keys {
		for _, seg := range strings.Split(k.path, ".") {
			r, n := utf8.DecodeRuneInString(seg)
			name.WriteRune(unicode.ToUpper(r))
			name.WriteString(seg[n:])
		}
		parts[i] = k.path
		if k.desc {
			parts[i] += " desc"
		}
	}
	return "index " + name.String() + "(" + strings.Join(parts, ", ") + ")"
}

// indexEdit inserts an index declaration after the last field or index.
func indexEdit(pass *Pass, d *types.Decl, decl string) (diag.Edit, bool) {
	var members []ast.Member
	switch n := d.Node.(type) {
	case *ast.EntityDecl:
		members = n.Members
	case *ast.ProjectionDecl:
		members = n.Members
	}
	var last ast.Member
	for _, m := range members {
		switch m.(type) {
		case *ast.FieldDecl, *ast.IndexDecl, *ast.IDDecl:
			last = m
		}
	}
	if last == nil {
		return diag.Edit{}, false
	}
	f := pass.File(last.End())
	if f == nil {
		return diag.Edit{}, false
	}
	line := f.Line(last.End())
	if line >= f.LineCount() {
		return diag.Edit{}, false
	}
	pos := f.LineStart(line + 1)
	return diag.Edit{Range: source.Range{Start: pos, End: pos}, NewText: "  " + decl + "\n"}, true
}

// personalKeys: an index on a personal field without desc (E0905).
func personalKeys(pass *Pass, d *types.Decl, ix *ast.IndexDecl) {
	for _, k := range ix.Keys {
		if v := d.Field(k.Path.Elems[0].Name); v != nil && v.Personal && k.Desc() {
			pass.Report(diag.E0905, rangeOf(k), k.Path.String())
		}
	}
}
