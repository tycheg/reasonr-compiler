package sema

import (
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// UI texts (RFC §22.11): layout strings in the app's base language and
// texts blocks with keys that are paths to model names and nodes.

// Text is a text in a language.
type Text struct {
	Lang  string
	Key   string
	Value string   // the text with {parameter} placeholders
	Node  ast.Node // *ast.TextEntry or a layout string
	// Layout: a layout string rather than a texts block.
	Layout bool
}

var (
	textKeyName     = "the model has no %q"
	textKeyMember   = "%s has no %q"
	textKeyNode     = "%s has no node %q"
	textKeyScreen   = "no screen or app %q"
	textKeyDup      = "text %q in language %s is already given"
	textPlaceholder = "placeholder {%s}: %s has no such parameter"
	textNoSubst     = "placeholders are only allowed in texts of errors and notifications: {%s}"
	textSubstExpr   = "a placeholder is a parameter name: {name}"
)

func (u *UI) addText(t *Text) bool {
	m := u.Texts[t.Lang]
	if m == nil {
		m = map[string]*Text{}
		u.Texts[t.Lang] = m
	}
	if m[t.Key] != nil {
		return false
	}
	m[t.Key] = t
	return true
}

// checkTexts collects layout strings, checks texts blocks and warns about
// texts without a translation (W1303).
func (c *checker) checkTexts(u *UI) {
	for _, a := range u.Apps {
		lang := a.Language
		if t := a.Node.Title; t != nil {
			u.addText(&Text{Lang: lang, Key: a.Name, Value: t.Value, Node: t, Layout: true})
		}
		layoutTexts(u, lang, a.Name, a.Nodes)
		for _, s := range a.Screens {
			if t := s.Node.Title; t != nil {
				u.addText(&Text{Lang: lang, Key: s.Name, Value: t.Value, Node: t, Layout: true})
			}
			layoutTexts(u, lang, s.Name, s.Nodes)
		}
	}
	for _, d := range c.prog.Decls {
		td, ok := d.(*ast.TextsDecl)
		if !ok {
			continue
		}
		lang := td.Lang.Name
		for _, e := range td.Entries {
			params, allowed, key, ok := c.textKey(u, e.Key)
			if !ok {
				continue
			}
			value := c.textValue(e, params, allowed)
			if !u.addText(&Text{Lang: lang, Key: key, Value: value, Node: e}) {
				c.bag.Add(diag.E0713, rangeOf(e.Key), diag.Message{Format: textKeyDup, Args: []any{key, lang}}).
					Note(rangeOf(u.Texts[lang][key].Node), noteDeclared)
			}
		}
	}
	c.translations(u)
}

func layoutTexts(u *UI, lang, owner string, nodes []*Node) {
	for _, n := range nodes {
		if n.Label != "" && n.Key != "" {
			var lit ast.Node = n.AST
			switch x := n.AST.(type) {
			case *ast.PrimNode:
				lit = x.Label
			case *ast.FieldNode:
				lit = x.Label
			}
			u.addText(&Text{Lang: lang, Key: owner + ":" + n.Key, Value: n.Label, Node: lit, Layout: true})
		}
		layoutTexts(u, lang, owner, n.Children)
	}
}

// translations warns about texts of the apps' base languages that are
// missing from the model's other languages.
func (c *checker) translations(u *UI) {
	var langs []string
	for lang := range u.Texts {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	base := map[string]bool{}
	for _, a := range u.Apps {
		base[a.Language] = true
	}
	var missing []*Text
	var to []string
	for _, b := range langs {
		if !base[b] {
			continue
		}
		for _, l := range langs {
			if l == b || !c.hasTextsBlock(l) {
				continue
			}
			for _, t := range u.Texts[b] {
				if u.Texts[l][t.Key] == nil {
					missing = append(missing, t)
					to = append(to, l)
				}
			}
		}
	}
	for i, t := range missing {
		c.bag.Add(diag.W1303, rangeOf(t.Node), t.Key, to[i], to[i])
	}
}

func (c *checker) hasTextsBlock(lang string) bool {
	for _, d := range c.prog.Decls {
		if td, ok := d.(*ast.TextsDecl); ok && td.Lang.Name == lang {
			return true
		}
	}
	return false
}

// textKey checks a text key. It returns the parameters for placeholders
// (for errors and notifications), the key in full names
// (Ordering.Order.Cancel, Ordering.AdminOrder:Order.Edit) and ok == false
// if there is no key.
func (c *checker) textKey(u *UI, k *ast.TextKey) (params []*types.Var, allowed bool, key string, ok bool) {
	elems := k.Path.Elems
	if k.IsNode() {
		owner := k.Path.String()
		var nodes []*Node
		if s := u.find(k.Path, owner); s != nil {
			nodes, owner = s.Nodes, s.Name
		} else if a := u.apps[owner]; a != nil && len(elems) == 1 {
			nodes = a.Nodes
		} else {
			c.bag.Add(diag.E0713, rangeOf(k.Path), diag.Message{Format: textKeyScreen, Args: []any{owner}})
			return nil, false, "", false
		}
		want := k.Node.String()
		if k.N != nil && k.Number() > 1 {
			want += "#" + k.N.Raw
		}
		var keys []string
		if findKey(nodes, want, &keys) {
			return nil, false, owner + ":" + want, true
		}
		d := c.bag.Add(diag.E0713, rangeOf(k), diag.Message{Format: textKeyNode, Args: []any{owner, want}})
		if s := closest(want, keys); s != "" {
			d.Hint(hintDidYouMean, s)
		}
		return nil, false, "", false
	}
	d, used := c.lookupPrefix(k.Path, names(elems))
	if d == nil {
		if a := u.apps[elems[0].Name]; a != nil && len(elems) == 1 {
			return nil, false, a.Name, true
		}
	}
	if d == nil || d.Kind == types.PrimitiveDecl {
		c.bag.Add(diag.E0713, rangeOf(elems[0]), diag.Message{Format: textKeyName, Args: []any{elems[0].Name}})
		return nil, false, "", false
	}
	rest := elems[used:]
	keyOf := func(owner *types.Decl) string {
		return owner.Name + strings.TrimPrefix(k.Path.String(), strings.Join(names(elems[:used]), "."))
	}
	if len(rest) == 0 {
		return nil, false, keyOf(d), true
	}
	name := rest[0].Name
	var m *types.Method
	switch {
	case len(rest) > 2:
	case d.Field(name) != nil && len(rest) == 1, d.Value(name) != nil && len(rest) == 1:
		return nil, false, keyOf(d), true
	case d.Kind == types.ScreenDecl && len(rest) == 1:
		for _, p := range d.Params {
			if p.Name == name {
				return nil, false, keyOf(d), true
			}
		}
	default:
		d, m, _ = c.entityMethod(d, name, k.Path)
	}
	if m != nil && m.Kind != types.Event && m.Kind != types.Rule && m.Kind != types.ValueMethod {
		if len(rest) == 1 {
			allowed = m.Kind == types.Error || m.Kind == types.Notification
			return m.Params, allowed, keyOf(d), true
		}
		for _, p := range m.Params {
			if p.Name == rest[1].Name {
				return nil, false, keyOf(d), true
			}
		}
		c.bag.Add(diag.E0713, rangeOf(rest[1]), diag.Message{Format: textKeyMember, Args: []any{d.Short + "." + m.Name, rest[1].Name}})
		return nil, false, "", false
	}
	c.bag.Add(diag.E0713, rangeOf(rest[0]), diag.Message{Format: textKeyMember, Args: []any{d.Short, names2(rest)}})
	return nil, false, "", false
}

func names2(ids []*ast.Ident) string { return strings.Join(names(ids), ".") }

func findKey(nodes []*Node, want string, keys *[]string) bool {
	for _, n := range nodes {
		if n.Key != "" {
			if n.Key == want {
				return true
			}
			*keys = append(*keys, n.Key)
		}
		if findKey(n.Children, want, keys) {
			return true
		}
	}
	return false
}

// textValue checks {parameter} placeholders and returns the text.
func (c *checker) textValue(e *ast.TextEntry, params []*types.Var, allowed bool) string {
	switch v := e.Value.(type) {
	case *ast.BasicLit:
		return v.Value
	case *ast.InterpString:
		var b strings.Builder
		for _, part := range v.Parts {
			if part.X == nil {
				b.WriteString(part.Text)
				continue
			}
			id, ok := part.X.(*ast.Ident)
			switch {
			case !ok:
				c.bag.Add(diag.E0714, rangeOf(part.X), textSubstExpr)
				continue
			case !allowed:
				c.bag.Add(diag.E0714, rangeOf(id), diag.Message{Format: textNoSubst, Args: []any{id.Name}})
			default:
				found := false
				for _, p := range params {
					found = found || p.Name == id.Name
				}
				if !found {
					c.bag.Add(diag.E0714, rangeOf(id), diag.Message{Format: textPlaceholder, Args: []any{id.Name, e.Key.Path.String()}})
				}
			}
			b.WriteString("{" + id.Name + "}")
		}
		return b.String()
	}
	return ""
}
