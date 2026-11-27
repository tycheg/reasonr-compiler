package sema

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// The automatic UI of an app (RFC §22.9; RFC §22). The
// app … auto layout is built as language text, parsed and checked the
// common way, as if written: reasonr ui eject prints exactly what runs.
//
//  1. Take the public queries and commands open to the app's members: the
//     method's access names an actor or a role from app(…), or there is no
//     access list.
//  2. A type with a list (a page or list query) or a value without
//     identifier parameters gets a screen in the menu: Table or List for
//     every such query, Card for every value.
//  3. A type X with a value query that has an X.Id parameter gets a card
//     screen with an X.Id parameter: Card for every such query, X's commands
//     in Actions.
//  4. Rows with an X.Id field open the card of X.
//  5. Commands targeting me go into the Actions of the app actor's value;
//     commands whose target cannot be taken from a scope become a Form in
//     the menu: the target is a form field if the identifier has a default,
//     otherwise new (creation).
//  6. A command that has no place does not go into the app.

// autoTypeView is what the automatic UI places for a type.
type autoTypeView struct {
	decl     *types.Decl
	lists    []*types.Method // page and list queries without identifier parameters
	values   []*types.Method // value queries without identifier parameters
	cards    []*types.Method // value queries with an X.Id parameter
	commands []*types.Method
	menu     string // the menu screen or ""
	card     string // the card screen or ""
	param    string // the card screen parameter
}

// autoApp builds the layout text of an auto app: the root and the screens.
func (c *checker) autoApp(u *UI, a *App) []byte {
	g := &autoGen{c: c, u: u, a: a, used: map[string]bool{}}
	for name, d := range c.info.Decls {
		g.used[name] = true
		g.used[d.Short] = true
	}
	for _, s := range c.stdPrims {
		g.used[s.Name] = true
	}
	var views []*autoTypeView
	for _, d := range c.info.DeclList {
		if !d.Kind.HasID() {
			continue
		}
		v := &autoTypeView{decl: d}
		for _, m := range d.Methods {
			if !m.Public() || !c.access(a, d, m, false) {
				continue
			}
			switch {
			case m.Kind == types.Command:
				v.commands = append(v.commands, m)
			case idParam(m) == nil && isListResult(m.Result):
				v.lists = append(v.lists, m)
			case idParam(m) == nil && isValueResult(m.Result):
				v.values = append(v.values, m)
			case isValueResult(m.Result) && ownIDParam(m, d) != nil:
				v.cards = append(v.cards, m)
			}
		}
		if len(v.lists)+len(v.values) > 0 {
			v.menu = g.name(a.Name + d.Short)
		}
		if len(v.cards) > 0 {
			v.card = g.name(a.Name + d.Short + "Card")
			v.param = ownIDParam(v.cards[0], d).Name
		}
		views = append(views, v)
	}
	g.views = views
	var screens, menu []string
	for _, v := range views {
		if v.menu != "" {
			menu = append(menu, v.menu)
			screens = append(screens, g.menuScreen(v))
		}
		if v.card != "" {
			screens = append(screens, g.cardScreen(v))
		}
	}
	for _, v := range views {
		for _, m := range v.commands {
			if g.placed[m] {
				continue
			}
			if s := g.formScreen(v, m); s != "" {
				menu = append(menu, g.lastForm)
				screens = append(screens, s)
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# The automatic UI of app %s (reasonr ui eject %s)\n\n", a.Name, a.Name)
	b.WriteString(c.appHeaderText(a))
	if len(menu) > 0 {
		b.WriteString("  Menu\n")
		for _, s := range menu {
			b.WriteString("    " + s + "\n")
		}
	}
	for _, s := range screens {
		b.WriteString("\n" + s)
	}
	return []byte(b.String())
}

type autoGen struct {
	c        *checker
	u        *UI
	a        *App
	used     map[string]bool
	views    []*autoTypeView
	placed   map[*types.Method]bool
	lastForm string
}

// name is a free screen name: a taken one gets a number appended.
func (g *autoGen) name(base string) string {
	name := base
	for i := 2; g.used[name]; i++ {
		name = base + strconv.Itoa(i)
	}
	g.used[name] = true
	return name
}

func (g *autoGen) place(m *types.Method) {
	if g.placed == nil {
		g.placed = map[*types.Method]bool{}
	}
	g.placed[m] = true
}

// appHeaderText is the app heading without auto, and its language,
// platforms and default lines as written.
func (c *checker) appHeaderText(a *App) string {
	var names []string
	for _, id := range a.Node.Access.Names {
		names = append(names, id.Name)
	}
	head := "app(" + strings.Join(names, ", ") + ") " + a.Name
	if a.Title != "" {
		head += " " + strconv.Quote(a.Title)
	}
	head += "\n"
	for _, it := range a.Node.Items {
		if _, node := it.(ast.UINode); !node {
			head += "  " + c.sourceText(it) + "\n"
		}
	}
	return head
}

// sourceText is the source text of a node.
func (c *checker) sourceText(n ast.Node) string {
	for _, f := range c.files {
		if f.Contains(n.Pos()) && f.Contains(n.End()) {
			return string(f.Source()[f.Offset(n.Pos()):f.Offset(n.End())])
		}
	}
	return ""
}

func (g *autoGen) menuScreen(v *autoTypeView) string {
	var b strings.Builder
	b.WriteString("screen " + v.menu + "\n")
	d := v.decl
	for _, m := range v.lists {
		prim := "List"
		if _, page := m.Result.(*types.Page); page {
			prim = "Table"
		}
		b.WriteString("  " + prim + " " + d.Short + "." + m.Name + "\n")
		if open := g.openRow(elemOf(m.Result)); open != "" {
			b.WriteString("    " + open + "\n")
		}
	}
	for _, m := range v.values {
		b.WriteString("  Card " + d.Short + "." + m.Name + "\n")
		if g.a.Me != nil && sameEntity(g.a.Me, d) {
			g.actions(&b, v, "    ")
		}
	}
	return b.String()
}

// openRow is a transition from a row with an X.Id field to the card of X (item 4).
func (g *autoGen) openRow(row types.Type) string {
	rv, ok := row.(*types.Value)
	if !ok {
		return ""
	}
	for _, f := range rv.Decl.Fields {
		id, ok := types.Unwrap(f.Type).(*types.ID)
		if !ok {
			continue
		}
		for _, v := range g.views {
			if sameEntity(v.decl, id.Owner) && v.card != "" {
				if f.Name == v.param {
					return "on open " + v.card
				}
				return "on open " + v.card + "(" + v.param + ": " + f.Name + ")"
			}
		}
	}
	return ""
}

func (g *autoGen) cardScreen(v *autoTypeView) string {
	var b strings.Builder
	d := v.decl
	fmt.Fprintf(&b, "screen %s(%s %s.Id)\n", v.card, v.param, d.Short)
	for i, m := range v.cards {
		args := ""
		if p := ownIDParam(m, d); p.Name != v.param {
			args = "(" + p.Name + ": " + v.param + ")"
		}
		b.WriteString("  Card " + d.Short + "." + m.Name + args + "\n")
		if i == 0 {
			g.actions(&b, v, "    ")
		}
	}
	return b.String()
}

// actions is Actions with the type's commands: Edit if there is something to
// enter, Button otherwise (items 3, 5).
func (g *autoGen) actions(b *strings.Builder, v *autoTypeView, indent string) {
	var lines []string
	for _, m := range v.commands {
		if g.placed[m] {
			continue
		}
		args, ok := g.idArgs(m, nil)
		if !ok {
			continue
		}
		prim := "Button"
		if g.hasInputs(m, args) {
			prim = "Edit"
		}
		lines = append(lines, indent+"  "+prim+" "+v.decl.Short+"."+m.Name+args)
		g.place(m)
	}
	if len(lines) > 0 {
		b.WriteString(indent + "Actions\n" + strings.Join(lines, "\n") + "\n")
	}
}

// formScreen is a screen with the form of a command whose target is not from a scope (item 5).
func (g *autoGen) formScreen(v *autoTypeView, m *types.Method) string {
	d := v.decl
	target := d.Short + "." + m.Name
	switch {
	case g.a.Me != nil && sameEntity(g.a.Me, d):
	case g.u.DefaultFor([]*App{g.a}, d.ID, RoleInput) != nil:
	case isBasic(d.ID.Base, types.UUID) || isBasic(d.ID.Base, types.Bigint):
		target = "[new " + d.Short + ".Id]." + m.Name
	default:
		return "" // the caller sets the identifier, and there is nothing to enter it with
	}
	args, ok := g.idArgs(m, d)
	if !ok {
		return ""
	}
	g.place(m)
	g.lastForm = g.name(g.a.Name + d.Short + m.Name)
	return "screen " + g.lastForm + "\n  Form " + target + args + "\n"
}

// idArgs are the identifier arguments that cannot be entered: new if the
// type has no default. A parameter of the type of entity owner itself is an
// argument, not a target. ok == false means new is impossible for the identifier.
func (g *autoGen) idArgs(m *types.Method, owner *types.Decl) (string, bool) {
	var args []string
	for _, p := range m.Params {
		id, isID := types.Unwrap(p.Type).(*types.ID)
		if !isID || isOptional(p.Type) || !required(p) {
			continue
		}
		if g.u.DefaultFor([]*App{g.a}, id, RoleInput) != nil {
			continue
		}
		if !isBasic(id.Base, types.UUID) && !isBasic(id.Base, types.Bigint) {
			return "", false
		}
		args = append(args, p.Name+": new")
	}
	if len(args) == 0 {
		return "", true
	}
	return "(" + strings.Join(args, ", ") + ")", true
}

// hasInputs: the command has parameters the user enters.
func (g *autoGen) hasInputs(m *types.Method, args string) bool {
	for _, p := range m.Params {
		if !strings.Contains(args, p.Name+": ") {
			return true
		}
	}
	return false
}

func idParam(m *types.Method) *types.Var {
	for _, p := range m.Params {
		if _, ok := types.Unwrap(p.Type).(*types.ID); ok {
			return p
		}
	}
	return nil
}

func ownIDParam(m *types.Method, d *types.Decl) *types.Var {
	for _, p := range m.Params {
		if id, ok := types.Unwrap(p.Type).(*types.ID); ok && sameEntity(id.Owner, d) {
			return p
		}
	}
	return nil
}

func isListResult(t types.Type) bool {
	switch t := t.(type) {
	case *types.Page:
		_, ok := t.Elem.(*types.Value)
		return ok
	case *types.List:
		_, ok := t.Elem.(*types.Value)
		return ok
	}
	return false
}

func isValueResult(t types.Type) bool {
	_, ok := t.(*types.Value)
	return ok
}

// autoApps builds the layout of auto apps and puts it into the declarations
// (copies): from there it is checked as if written.
func (c *checker) autoApps(u *UI) {
	for _, a := range u.Apps {
		if !a.Auto {
			continue
		}
		src := c.autoApp(u, a)
		a.AutoSource = src
		f, diags := c.prog.Generate("auto/"+a.Name+".rr", src)
		c.bag.Append(diags...)
		if f == nil {
			continue
		}
		cp := *a.Node
		cp.Items = nil
		for _, d := range f.AST.Decls {
			switch d := d.(type) {
			case *ast.AppDecl:
				cp.Items = d.Items
			case *ast.ScreenDecl:
				td := &types.Decl{Kind: types.ScreenDecl, Name: d.Name.Name, Short: d.Name.Name, Node: d}
				if d.Params != nil {
					td.Params = c.params(d.Params)
				}
				c.info.Decls[td.Name] = td
				c.info.DeclList = append(c.info.DeclList, td)
				s := &Screen{Name: td.Name, Decl: td, Node: d, Params: td.Params}
				u.Screens = append(u.Screens, s)
				u.screens[s.Name] = s
			}
		}
		a.Node = &cp
	}
}

// sameEntity reports whether a is the same entity, or actor a acts as actor
// b through an ACL (actor Auth.User as Customer): then me of an app of actor
// a is an identifier of b with the same value.
func sameEntity(a, b *types.Decl) bool { return a == b || actsAs(a, b) }
