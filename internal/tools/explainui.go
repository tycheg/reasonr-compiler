package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// UI description (RFC §22): for an app, its screens,
// places and the methods there is no place for; for a screen, its layout
// and the source of every argument (RFC §22.4).

var (
	lblEnters    = "Enter"
	lblLanguage  = "Language %s, platforms %s"
	lblAuto      = "The interface is built automatically (auto)"
	lblScreens   = "Screens"
	lblAppPlaces = "Places"
	lblNotInUI   = "Not in the interface"
	lblInApps    = "Apps"
	lblNoApps    = "no app reaches the screen"
	lblLayout    = "Layout"
	lblPlace     = "place %s"
	lblTarget    = "target"
	lblAutoParts = "automatically: %s"
	lblDefault   = "default for %s"
	lblBinds     = "Binds"
	lblParts     = "Parts"
	lblEvents    = "Events"
	lblUsed      = "Used on screens"

	srcExpr      = "= %s"
	srcParam     = "← screen parameter %s"
	srcMe        = "← me"
	srcNew       = "← new — a new id"
	srcData      = "← data %s.%s"
	srcInitial   = "← input, initial value from %s"
	srcRow       = "← row %s.%s"
	srcForm      = "← form %s.%s"
	srcEvent     = "← event %s"
	srcProvided  = "← primitive value %s"
	srcPrimitive = "← the primitive (paging)"
	srcInput     = "← input"
	srcInputDef  = "← input, the parameter default"
)

// explainUI describes an app, a screen of an app (Admin.AdminOrder), a
// screen or a primitive; ok == false means the name is not from the UI.
func (e *explainer) explainUI(d *types.Decl, member string, hasMember bool) (bool, error) {
	u := e.info.UI
	if u == nil || !d.Kind.IsUI() {
		return false, nil
	}
	switch d.Kind {
	case types.AppDecl:
		a := u.App(d.Name)
		if !hasMember {
			e.app(a)
			return true, nil
		}
		var s *sema.Screen
		for _, as := range a.Screens {
			if as.Name == member || as.Decl.Short == member {
				s = as
			}
		}
		if s == nil {
			return true, fmt.Errorf("app %s has no screen %q", a.Name, member)
		}
		e.screen(s)
		return true, nil
	case types.ScreenDecl:
		e.screen(u.Screen(d.Name))
	case types.PrimitiveDecl:
		e.primitive(u.Primitive(d.Name))
	}
	return true, nil
}

func (e *explainer) app(a *sema.App) {
	head := "app " + a.Name
	if a.Title != "" {
		head += ` "` + a.Title + `"`
	}
	e.line(0, "%s", head)
	e.doc(a.Node.Doc)
	var enters []string
	for _, x := range a.Actors {
		enters = append(enters, x.Name)
	}
	enters = append(enters, a.Roles...)
	e.line(0, "%s: %s", e.t(lblEnters), strings.Join(enters, ", "))
	e.line(0, "%s", e.t(lblLanguage, a.Language, strings.Join(a.Platforms, ", ")))
	if a.Auto {
		e.line(0, "%s", e.t(lblAuto))
		return
	}
	if len(a.Screens) > 0 {
		e.line(0, "%s:", e.t(lblScreens))
		for _, s := range a.Screens {
			e.line(1, "%s", screenHead(s))
		}
	}
	if len(a.Places) > 0 {
		e.line(0, "%s:", e.t(lblAppPlaces))
		for _, p := range a.Places {
			e.line(1, "%s — %s %s.%s", p.Name, p.Node.Method.Kind, p.Node.Owner.Name, p.Node.Method.Name)
		}
	}
	if missing := e.notInUI(a); len(missing) > 0 {
		e.line(0, "%s:", e.t(lblNotInUI))
		for _, m := range missing {
			e.line(1, "%s", m)
		}
	}
}

// notInUI lists the public commands and queries available to the app's
// members that are on none of its screens (RFC §22.9, item 6).
func (e *explainer) notInUI(a *sema.App) []string {
	used := map[*types.Method]bool{}
	for _, p := range a.Places {
		used[p.Node.Method] = true
	}
	for _, m := range e.info.UI.Methods() {
		used[m] = true
	}
	var out []string
	for _, d := range e.info.DeclList {
		if !d.Kind.HasID() {
			continue
		}
		for _, m := range d.Methods {
			if m.Public() && !used[m] && e.callableIn(a, d, m) {
				out = append(out, m.Kind.String()+" "+d.Name+"."+m.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// callableIn: the method is available to one of the members: an actor type
// or a role in the method's access list.
func (e *explainer) callableIn(a *sema.App, owner *types.Decl, m *types.Method) bool {
	var access *ast.AccessList
	switch n := m.Node.(type) {
	case *ast.CommandDecl:
		access = n.Access
	case *ast.QueryDecl:
		access = n.Access
	}
	if access == nil {
		return false // without access it is not for people in the UI
	}
	for _, id := range access.Names {
		acc := e.info.Access[id]
		switch acc.Kind {
		case sema.AccessRole:
			// Actors have roles too: app(Staff) includes staff with roles.
			if len(a.Actors) > 0 {
				return true
			}
			for _, r := range a.Roles {
				if r == e.info.RoleNames[acc.Role] {
					return true
				}
			}
		case sema.AccessActor:
			for _, x := range a.Actors {
				if x == acc.Actor {
					return true
				}
			}
		case sema.AccessSelf:
			for _, x := range a.Actors {
				if x == owner {
					return true
				}
			}
		}
	}
	return false
}

func screenHead(s *sema.Screen) string {
	head := s.Name
	if len(s.Params) > 0 {
		var ps []string
		for _, p := range s.Params {
			ps = append(ps, p.Name+" "+p.Type.String())
		}
		head += "(" + strings.Join(ps, ", ") + ")"
	}
	if s.Title != "" {
		head += ` "` + s.Title + `"`
	}
	return head
}

func (e *explainer) screen(s *sema.Screen) {
	e.line(0, "screen %s", screenHead(s))
	e.doc(s.Node.Doc)
	var apps []string
	for _, a := range s.Apps {
		apps = append(apps, a.Name)
	}
	if len(apps) == 0 {
		e.line(0, "%s: %s", e.t(lblInApps), e.t(lblNoApps))
	} else {
		e.line(0, "%s: %s", e.t(lblInApps), strings.Join(apps, ", "))
	}
	e.line(0, "%s:", e.t(lblLayout))
	e.nodes(s, s.Nodes, 1)
}

func (e *explainer) nodes(s *sema.Screen, nodes []*sema.Node, indent int) {
	for _, n := range nodes {
		e.line(indent, "%s", e.nodeHead(s, n))
		if n.Target != nil {
			e.line(indent+1, "%s %s", e.t(lblTarget), e.source(n.Target))
		}
		for _, a := range n.Args {
			e.line(indent+1, "%s %s", a.Name, e.source(a))
		}
		for _, a := range n.ToArgs {
			e.line(indent+1, "%s %s", a.Name, e.source(a))
		}
		if len(n.Auto) > 0 {
			e.line(indent+1, "%s", e.t(lblAutoParts, strings.Join(n.Auto, ", ")))
		}
		e.nodes(s, n.Children, indent+1)
	}
}

func (e *explainer) nodeHead(s *sema.Screen, n *sema.Node) string {
	var b strings.Builder
	switch n.Kind {
	case sema.NodePart:
		b.WriteString(n.Part.Name)
	case sema.NodeScreen:
		b.WriteString(n.To.Name)
	case sema.NodeOn:
		b.WriteString("on " + n.Event.Name + " ")
		if n.Back {
			b.WriteString("back")
		} else {
			b.WriteString(n.To.Name)
		}
	case sema.NodeField:
		b.WriteString(n.Field)
		if n.Prim != nil {
			b.WriteString(" " + n.Prim.Name)
		}
		if n.Default != nil {
			b.WriteString(" (" + e.t(lblDefault, n.Default.Pattern.String()) + ")")
		}
	default:
		b.WriteString(n.Prim.Name)
		switch {
		case n.Method != nil:
			b.WriteString(" " + n.Owner.Name + "." + n.Method.Name)
		case n.To != nil:
			b.WriteString(" " + n.To.Name)
		}
	}
	if n.Label != "" {
		b.WriteString(` "` + n.Label + `"`)
	}
	if n.Cond != nil {
		b.WriteString(" if " + e.src(n.Cond.X))
	}
	if n.Bound() {
		for _, a := range s.Apps {
			b.WriteString(" — " + e.t(lblPlace, a.Name+"."+s.Name+":"+n.Key))
		}
	}
	return b.String()
}

func (e *explainer) source(a *sema.Arg) string {
	from := ""
	if a.From != nil {
		from = a.From.Key
	}
	if a.Input && a.Source != sema.SrcInput {
		a := *a
		a.Input = false
		return e.t(srcInitial, strings.TrimPrefix(e.source(&a), "← "))
	}
	switch a.Source {
	case sema.SrcExpr:
		return e.t(srcExpr, e.src(a.Expr))
	case sema.SrcParam:
		return e.t(srcParam, a.Field)
	case sema.SrcMe:
		return e.t(srcMe)
	case sema.SrcNew:
		return e.t(srcNew)
	case sema.SrcData:
		return e.t(srcData, from, a.Field)
	case sema.SrcRow:
		return e.t(srcRow, from, a.Field)
	case sema.SrcForm:
		return e.t(srcForm, from, a.Field)
	case sema.SrcEvent:
		return e.t(srcEvent, a.Field)
	case sema.SrcProvided:
		return e.t(srcProvided, a.Field)
	case sema.SrcPrimitive:
		return e.t(srcPrimitive)
	}
	if a.Default {
		return e.t(srcInputDef)
	}
	return e.t(srcInput)
}

func (e *explainer) primitive(p *sema.Primitive) {
	e.line(0, "%s", e.uiSrcHead(p))
	e.doc(p.Node.Doc)
	var binds string
	switch p.Binds {
	case sema.BindNone:
		binds = "—"
	case sema.BindQuery:
		binds = p.BindType.String()
	case sema.BindCommand:
		binds = "command"
	case sema.BindScreen:
		binds = "screen"
	case sema.BindField:
		binds = "field " + p.BindType.String()
	case sema.BindInput:
		binds = "input " + p.BindType.String()
	}
	e.line(0, "%s: %s", e.t(lblBinds), binds)
	if len(p.Parts) > 0 {
		var parts []string
		for _, pt := range p.Parts {
			parts = append(parts, pt.Name)
		}
		e.line(0, "%s: %s", e.t(lblParts), strings.Join(parts, ", "))
	}
	if len(p.Events) > 0 {
		var evs []string
		for _, ev := range p.Events {
			evs = append(evs, ev.Name)
		}
		e.line(0, "%s: %s", e.t(lblEvents), strings.Join(evs, ", "))
	}
	var used []string
	for _, s := range e.info.UI.Screens {
		if usesPrim(s.Nodes, p) {
			used = append(used, s.Name)
		}
	}
	if len(used) > 0 {
		e.line(0, "%s: %s", e.t(lblUsed), strings.Join(used, ", "))
	}
}

// uiSrcHead is the heading of a primitive declaration from the module or the catalog.
func (e *explainer) uiSrcHead(p *sema.Primitive) string {
	n := p.Node
	end := n.Name.End()
	if n.Params != nil {
		end = n.Params.End()
	} else if n.TypeParams != nil {
		end = n.TypeParams.End()
	}
	if f := e.m.Module.Std; p.Std && f != nil {
		src := f.Src.Source()
		return string(src[f.Src.Offset(n.Primitive):f.Src.Offset(end)])
	}
	return e.m.Source(rangeOf(&ast.BadNode{From: n.Primitive, To: end}))
}

func usesPrim(nodes []*sema.Node, p *sema.Primitive) bool {
	for _, n := range nodes {
		if n.Prim == p || usesPrim(n.Children, p) {
			return true
		}
	}
	return false
}
