package sema

import (
	"strconv"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// App platforms (RFC §22.2).
var platforms = map[string]bool{"web": true, "mobile": true}

var (
	textUILangTwice  = "language is given twice"
	textUIPlatTwice  = "platforms is given twice"
	textUIPlatform   = "unknown platform %q: web or mobile"
	textUIPlatRepeat = "platform %q is repeated"
	textUIAutoNodes  = "an auto app has no layout: the engine builds it (RFC §22.9)"
	textUIDefNoPrim  = "no primitive %q"
	textUIDefKind    = "default is for field display (binds field) and input (binds input) primitives, and %s is neither"
	textUIDefType    = "%s takes %s, but the default is declared for %s"
	textUIDefDup     = "a %s default for %s is already declared"
	roleShow         = "display"
	roleInput        = "input"
)

// checkUI checks the model UI (RFC §22): primitives, apps,
// default, the layout of screens and app roots, places, access and texts.
// It is called after the bodies, so method types are known.
func (c *checker) checkUI() {
	u := &UI{
		Texts:   map[string]map[string]*Text{},
		Nodes:   map[ast.UINode]*Node{},
		apps:    map[string]*App{},
		screens: map[string]*Screen{},
		prims:   map[string]*Primitive{},
	}
	c.info.UI = u
	u.find = func(at ast.Node, name string) *Screen {
		if d := c.lookup(at, name); d != nil && d.Kind == types.ScreenDecl {
			return u.screens[d.Name]
		}
		return nil
	}
	for _, d := range c.stdPrims {
		p := c.primitiveDecl(d, d.Node.(*ast.PrimitiveDecl), true)
		u.Primitives = append(u.Primitives, p)
		u.prims[p.Name] = p
	}
	for _, d := range c.info.DeclList {
		switch n := d.Node.(type) {
		case *ast.PrimitiveDecl:
			p := c.primitiveDecl(d, n, false)
			u.Primitives = append(u.Primitives, p)
			u.prims[p.Name] = p
		case *ast.ScreenDecl:
			s := &Screen{Name: d.Name, Decl: d, Node: n, Params: d.Params}
			if n.Title != nil {
				s.Title = n.Title.Value
			}
			u.Screens = append(u.Screens, s)
			u.screens[s.Name] = s
		case *ast.AppDecl:
			a := c.appHeader(d, n)
			u.Apps = append(u.Apps, a)
			u.apps[a.Name] = a
		}
	}
	for _, d := range c.prog.Decls {
		if n, ok := d.(*ast.DefaultDecl); ok {
			c.uiDefault(u, n, nil, false)
		}
	}
	for _, d := range c.prog.Std {
		if n, ok := d.(*ast.DefaultDecl); ok {
			c.uiDefault(u, n, nil, true)
		}
	}
	for _, a := range u.Apps {
		for _, it := range a.Node.Items {
			if n, ok := it.(*ast.DefaultDecl); ok {
				c.uiDefault(u, n, a, false)
			}
		}
	}
	for _, a := range u.Apps {
		if nodes := appNodes(a.Node); a.Auto && len(nodes) > 0 {
			c.bag.Add(diag.E0715, rangeOf(nodes[0]), textUIAutoNodes)
		}
	}
	c.autoApps(u)
	c.uiReach(u)
	for _, a := range u.Apps {
		l := &layout{c: c, ui: u, apps: []*App{a}, me: a.Me}
		a.Nodes = l.nodes((&uiCtx{}).only(textUIRoot), appNodes(a.Node), nil)
	}
	for _, s := range u.Screens {
		l := &layout{c: c, ui: u, screen: s, apps: s.Apps, me: s.Me}
		params := &uiScope{}
		for _, p := range s.Params {
			params.add(p.Name, p.Type, SrcParam, nil, p.Node, p)
		}
		s.Nodes = l.nodes((&uiCtx{scopes: []*uiScope{params}}).only(textUIRoot), s.Node.Nodes, nil)
	}
	for _, a := range u.Apps {
		assignKeys(a.Nodes)
		exprFuncs(a.Name, a.Nodes)
	}
	for _, s := range u.Screens {
		assignKeys(s.Nodes)
		exprFuncs(s.Name, s.Nodes)
		if len(s.Apps) == 0 {
			c.bag.Add(diag.W1302, rangeOf(s.Node.Name), s.Name)
		}
	}
	c.uiPlaces(u)
	c.checkTexts(u)
}

// appNodes are the layout lines of the app root.
func appNodes(n *ast.AppDecl) []ast.UINode {
	var out []ast.UINode
	for _, it := range n.Items {
		if node, ok := it.(ast.UINode); ok {
			out = append(out, node)
		}
	}
	return out
}

// appHeader collects the app heading: who enters, the language, platforms.
func (c *checker) appHeader(d *types.Decl, n *ast.AppDecl) *App {
	a := &App{Name: d.Name, Decl: d, Node: n, Language: "en", Auto: n.IsAuto()}
	if n.Title != nil {
		a.Title = n.Title.Value
	}
	if n.Access != nil {
		for _, id := range n.Access.Names {
			acc, ok := c.info.Access[id]
			switch {
			case !ok:
			case acc.Kind == AccessRole:
				a.Roles = append(a.Roles, c.info.RoleNames[acc.Role])
			case acc.Kind == AccessActor:
				a.Actors = append(a.Actors, acc.Actor)
			}
		}
		if t, ok := c.meType(n.Access).(*types.ID); ok {
			a.Me = t.Owner
		}
	}
	var lang, plat bool
	for _, it := range n.Items {
		switch it := it.(type) {
		case *ast.LanguageItem:
			if lang {
				c.bag.Add(diag.E0715, rangeOf(it), textUILangTwice)
				continue
			}
			lang = true
			a.Language = it.Name.Name
		case *ast.PlatformsItem:
			if plat {
				c.bag.Add(diag.E0715, rangeOf(it), textUIPlatTwice)
				continue
			}
			plat = true
			seen := map[string]bool{}
			for _, id := range it.Names {
				switch {
				case !platforms[id.Name]:
					c.bag.Add(diag.E0715, rangeOf(id), diag.Message{Format: textUIPlatform, Args: []any{id.Name}})
				case seen[id.Name]:
					c.bag.Add(diag.E0715, rangeOf(id), diag.Message{Format: textUIPlatRepeat, Args: []any{id.Name}})
				default:
					seen[id.Name] = true
					a.Platforms = append(a.Platforms, id.Name)
				}
			}
		}
	}
	if len(a.Platforms) == 0 {
		a.Platforms = []string{"web"}
	}
	return a
}

// uiDefault checks a default: of the module (app == nil, std == false), of
// the catalog or of an app.
func (c *checker) uiDefault(u *UI, n *ast.DefaultDecl, app *App, std bool) {
	pat := c.uiType(n.Type, nil, nil, false)
	if types.IsInvalid(pat) {
		return
	}
	p := u.prims[n.Prim.Name]
	if p == nil {
		c.bag.Add(diag.E0712, rangeOf(n.Prim), diag.Message{Format: textUIDefNoPrim, Args: []any{n.Prim.Name}})
		return
	}
	d := &Default{Node: n, Pattern: pat, Prim: p, App: app, Std: std}
	switch p.Binds {
	case BindField:
		d.Role = RoleShow
	case BindInput:
		d.Role = RoleInput
	default:
		c.bag.Add(diag.E0712, rangeOf(n.Prim), diag.Message{Format: textUIDefKind, Args: []any{p.Name}})
		return
	}
	s := subst{}
	if !unify(p.BindType, pat, s) {
		c.bag.Add(diag.E0712, rangeOf(n), diag.Message{Format: textUIDefType, Args: []any{p.Name, p.BindType.String(), pat.String()}})
		return
	}
	var apps []*App
	if app != nil {
		apps = []*App{app}
	}
	l := &layout{c: c, ui: u, apps: apps}
	node := &Node{Prim: p}
	l.primArgs((&uiCtx{}).only(textUIRoot), node, n.Args, s, n.Prim, true)
	d.Params, d.Subst = node.Params, node.Subst
	level := u.Defaults
	if app != nil {
		level = app.Defaults
	}
	for _, prev := range level {
		if prev.Std == std && prev.Role == d.Role && samePattern(prev.Pattern, pat) {
			role := roleShow
			if d.Role == RoleInput {
				role = roleInput
			}
			c.bag.Add(diag.E0712, rangeOf(n), diag.Message{Format: textUIDefDup, Args: []any{role, pat.String()}}).
				Note(rangeOf(prev.Node), noteDeclared)
			return
		}
	}
	if app != nil {
		app.Defaults = append(app.Defaults, d)
	} else {
		u.Defaults = append(u.Defaults, d)
	}
}

func samePattern(a, b types.Type) bool {
	return unify(a, b, subst{}) && unify(b, a, subst{})
}

// uiReach finds the screens of every app: the menu, transitions, links and
// embeddings from the root (RFC §22.2).
func (c *checker) uiReach(u *UI) {
	for _, a := range u.Apps {
		queue := u.refs(appNodes(a.Node), nil)
		seen := map[*Screen]bool{}
		for len(queue) > 0 {
			s := queue[0]
			queue = queue[1:]
			if s == nil || seen[s] {
				continue
			}
			seen[s] = true
			a.Screens = append(a.Screens, s)
			s.Apps = append(s.Apps, a)
			queue = append(queue, u.refs(s.Node.Nodes, nil)...)
		}
	}
	for _, s := range u.Screens {
		for i, a := range s.Apps {
			if a.Me == nil || (i > 0 && s.Me != a.Me) {
				s.Me = nil
				break
			}
			s.Me = a.Me
		}
	}
}

// refs are the screens lines refer to: embeddings, navigation items, Link,
// on transitions.
func (u *UI) refs(nodes []ast.UINode, parent *Primitive) []*Screen {
	var out []*Screen
	for _, n := range nodes {
		switch n := n.(type) {
		case *ast.PrimNode:
			name := n.Name.Name
			switch {
			case parent != nil && parent.Part(name) != nil:
				out = append(out, u.refs(n.Children, nil)...)
			case u.prims[name] != nil:
				if b := n.Binding; b != nil && b.IsScreen() && b.Target == nil {
					out = append(out, u.find(b.Type, b.Type.Name))
				}
				out = append(out, u.refs(n.Children, u.prims[name])...)
			case u.find(n.Name, name) != nil:
				out = append(out, u.find(n.Name, name))
			}
		case *ast.FieldNode:
			var p *Primitive
			if n.Prim != nil {
				p = u.prims[n.Prim.Name]
			}
			out = append(out, u.refs(n.Children, p)...)
		case *ast.OnNode:
			if n.Screen != nil {
				out = append(out, u.find(n.Screen, n.Screen.Name))
			}
		}
	}
	return out
}

// assignKeys gives nodes keys for texts and places: the binding, the
// screen, the field or the primitive; the second such node on a screen is
// #2 (RFC §22.10, §22.11).
func assignKeys(nodes []*Node) {
	counts := map[string]int{}
	var walk func([]*Node)
	walk = func(list []*Node) {
		for _, n := range list {
			base := ""
			switch {
			case n.Method != nil:
				base = n.Owner.Short + "." + n.Method.Name
			case n.To != nil && n.Kind != NodeOn:
				base = n.To.Decl.Short
			case n.Kind == NodeField:
				base = n.Field
			case n.Kind == NodePart:
				base = n.Part.Name
			case n.Kind == NodePrimitive:
				base = n.Prim.Name
			}
			if base != "" {
				counts[base]++
				n.Key = base
				if k := counts[base]; k > 1 {
					n.Key += "#" + strconv.Itoa(k)
				}
			}
			walk(n.Children)
		}
	}
	walk(nodes)
}

// exprFuncs names the bytecode functions of non-constant layout
// expressions: ui.<screen>:<node key>.if, .<argument>, .<primitive parameter>.
func exprFuncs(owner string, nodes []*Node) {
	name := func(n *Node, what string, e *Expr) {
		if e != nil && !e.IsConst {
			e.Func = "ui." + owner + ":" + n.Key + "." + what
		}
	}
	for _, n := range nodes {
		name(n, "if", n.Cond)
		args := append(append([]*Arg(nil), n.allArgs()...), n.ToArgs...)
		for _, a := range args {
			if a.Source == SrcExpr {
				name(n, a.Name, a.Code)
			}
		}
		for _, pv := range n.Params {
			name(n, pv.Param.Name, pv.Value)
		}
		exprFuncs(owner, n.Children)
	}
}

// uiPlaces lists the places of apps and warns about nodes whose method is
// open to none of the app's members (W1301).
func (c *checker) uiPlaces(u *UI) {
	for _, a := range u.Apps {
		c.appPlaces(a, nil, a.Nodes)
		for _, s := range a.Screens {
			c.appPlaces(a, s, s.Nodes)
		}
	}
}

func (c *checker) appPlaces(a *App, s *Screen, nodes []*Node) {
	for _, n := range nodes {
		if n.Bound() {
			name := a.Name + ":" + n.Key
			if s != nil {
				name = a.Name + "." + screenInApp(a, s) + ":" + n.Key
			}
			a.Places = append(a.Places, &Place{Name: name, App: a, Screen: s, Node: n})
			if !c.callable(a, n.Owner, n.Method) {
				c.bag.Add(diag.W1301, nodeRange(n.AST), n.Owner.Name+"."+n.Method.Name, a.Name)
			}
		}
		c.appPlaces(a, s, n.Children)
	}
}

// callable reports whether one of the app's members can call the method:
// the method's access from the model (RFC §22.6). An app actor may carry
// roles (app(Staff) includes staff with the roles admin, support), so role
// access in an actor's app is allowed.
func (c *checker) callable(a *App, owner *types.Decl, m *types.Method) bool {
	return c.access(a, owner, m, true)
}

// access checks the access of the app's members to a method; actorRoles:
// the app actor may have any roles.
func (c *checker) access(a *App, owner *types.Decl, m *types.Method, actorRoles bool) bool {
	var access *ast.AccessList
	switch n := m.Node.(type) {
	case *ast.CommandDecl:
		access = n.Access
	case *ast.QueryDecl:
		access = n.Access
	}
	if access == nil {
		return true
	}
	for _, id := range access.Names {
		acc, ok := c.info.Access[id]
		if !ok {
			return true // the access error is already reported
		}
		var actor *types.Decl
		switch acc.Kind {
		case AccessRole:
			if actorRoles && len(a.Actors) > 0 {
				return true
			}
			for _, r := range a.Roles {
				if r == c.info.RoleNames[acc.Role] {
					return true
				}
			}
		case AccessActor:
			actor = acc.Actor
		case AccessSelf:
			actor = owner
		case AccessField:
			if t, ok := types.Unwrap(acc.Field.Type).(*types.ID); ok {
				actor = t.Owner
			}
		}
		for _, x := range a.Actors {
			if actor != nil && (x == actor || actsAs(x, actor)) {
				return true
			}
		}
	}
	return false
}

// actsAs reports whether actor x acts as actor d through an ACL (actor
// Auth.User as Customer).
func actsAs(x, d *types.Decl) bool {
	for _, from := range d.Callers {
		if from == x {
			return true
		}
	}
	return false
}

// screenInApp is the screen name in a place name: short if the app has no
// other screen with the same short name, full otherwise.
func screenInApp(a *App, s *Screen) string {
	for _, o := range a.Screens {
		if o != s && o.Decl.Short == s.Decl.Short {
			return s.Name
		}
	}
	return s.Decl.Short
}
