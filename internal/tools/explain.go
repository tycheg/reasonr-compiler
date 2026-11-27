package tools

import (
	"fmt"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Labels in the description.
var (
	lblPublic     = "public"
	lblPrivate    = "private"
	lblAccess     = "Access"
	lblAnyone     = "any authenticated caller"
	lblNoAccess   = "only other commands and timers"
	lblChecks     = "Checks"
	lblIf         = "if %s"
	lblExit       = "exit"
	lblExitNoErr  = "exit without changes"
	lblErrorsExit = "errors: %s, exit"
	lblErrors     = "errors: %s"
	lblReturns    = "result %s"
	lblActions    = "Actions in order"
	lblCalledFrom = "Called from"
	lblChanges    = "Changes fields"
	lblRaisedBy   = "Raised by commands"
	lblReactions  = "Reactions"
	lblPlaces     = "UI places"
	lblFields     = "Fields"
	lblIndexes    = "Indexes"
	lblMembers    = "Members"
	lblPersonal   = "personal"
	lblUsedIn     = "Used in"
	lblAnswersAs  = "Answers with role %s"
	lblFake       = "Fake in tests"
	lblFlags      = "Under feature flags"
	lblFlagOn     = "if %s is on (since %s)"
	lblFlagOff    = "if %s is off"
	lblNoActions  = "nothing"

	actEvent    = "event %s"
	actCommand  = "command %s"
	actOnTarget = "command %s on %s"
	actIfExists = " (unless none)"
	actNotify   = "notification %s"
	actNotifyTo = "notification %s → %s"
	actError    = "error %s"
	actRule     = "rule %s"
	actQuery    = "query %s"
	actRequest  = "external request %s"
	actUpsert   = "updates projection %s"
	actDelete   = "deletes projection row %s"
	actSchedule = "schedules a timer for %s"
	actTimerDel = "deletes timer %s"
	actForget   = "forgets subject %s"
	actFakeCall = "answers through the API: command %s on %s"
	fromCommand = "from command %s"
	fromTimer   = "by a timer set in %s"
)

// Explain describes a type or a method of a model: "Order", "Order.Cancel".
func Explain(m *pipeline.Model, name string) (string, error) {
	e := &explainer{m: m, info: m.Info}
	typeName, member, hasMember := splitName(m.Info, name)
	d := m.Info.Lookup(typeName)
	if d == nil && !hasMember && m.Info.UI != nil && m.Info.UI.Primitive(typeName) != nil {
		e.primitive(m.Info.UI.Primitive(typeName)) // a primitive of the standard catalog
		return e.b.String(), nil
	}
	if d == nil {
		return "", fmt.Errorf("the model has no type %q", typeName)
	}
	if ok, err := e.explainUI(d, member, hasMember); ok {
		return e.b.String(), err
	}
	if !hasMember {
		e.typ(d)
		return e.b.String(), nil
	}
	if mm := d.Method(member); mm != nil {
		e.method(d, mm)
		return e.b.String(), nil
	}
	return "", fmt.Errorf("%s has no method %q", d.Name, member)
}

// splitName splits "Type.method" into a type and a member: the type name
// may be full (Ordering.Order, Ordering.Money); the longest prefix that
// names a type is taken.
func splitName(info *sema.Info, name string) (typ, member string, hasMember bool) {
	if info.Lookup(name) != nil {
		return name, "", false
	}
	if i := strings.LastIndex(name, "."); i > 0 && info.Lookup(name[:i]) != nil {
		return name[:i], name[i+1:], true
	}
	typ, member, hasMember = strings.Cut(name, ".")
	return typ, member, hasMember
}

type explainer struct {
	m    *pipeline.Model
	info *sema.Info
	b    strings.Builder
}

func (e *explainer) t(t string, args ...any) string {
	return diag.Message{Format: t, Args: args}.String()
}

func (e *explainer) line(indent int, format string, args ...any) {
	e.b.WriteString(strings.Repeat("  ", indent))
	fmt.Fprintf(&e.b, format, args...)
	e.b.WriteByte('\n')
}

func (e *explainer) src(n ast.Node) string {
	return e.m.Source(rangeOf(n))
}

func (e *explainer) doc(g *ast.CommentGroup) {
	if text := g.Text(); text != "" {
		for _, l := range strings.Split(text, "\n") {
			e.line(1, "%s", l)
		}
	}
}

func (e *explainer) typ(d *types.Decl) {
	e.line(0, "%s %s", d.Kind, d.Name)
	e.doc(declDoc(d.Node))
	if d.Role != "" {
		e.line(0, "%s", e.t(lblAnswersAs, d.Role))
	}
	if len(d.Fields) > 0 {
		e.line(0, "%s:", e.t(lblFields))
		for _, f := range d.Fields {
			extra := ""
			if f.Personal {
				extra = " — " + e.t(lblPersonal)
			}
			e.line(1, "%s %s%s", f.Name, f.Type, extra)
		}
	}
	if len(d.Indexes) > 0 {
		e.line(0, "%s:", e.t(lblIndexes))
		for _, ix := range d.Indexes {
			e.line(1, "%s", strings.TrimPrefix(e.src(ix.Node), "index "))
		}
	}
	if len(d.Values) > 0 {
		for _, v := range d.Values {
			e.line(1, "%s", v.Name)
		}
	}
	if len(d.Methods) > 0 {
		e.line(0, "%s:", e.t(lblMembers))
		for _, md := range d.Methods {
			e.line(1, "%s %s%s", md.Kind, md.Name, e.params(md))
		}
	}
}

func (e *explainer) params(md *types.Method) string {
	parts := make([]string, len(md.Params))
	for i, p := range md.Params {
		parts[i] = p.Name + " " + p.Type.String()
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func (e *explainer) method(d *types.Decl, md *types.Method) {
	head := fmt.Sprintf("%s %s.%s%s", md.Kind, d.Name, md.Name, e.params(md))
	if _, void := md.Result.(*types.Void); !void && md.Result != nil {
		head += " " + md.Result.String()
	}
	if md.Kind == types.Command || md.Kind == types.Query {
		vis := e.t(lblPrivate)
		if md.Public() {
			vis = e.t(lblPublic)
		}
		head += " — " + vis
	}
	e.line(0, "%s", head)
	e.doc(memberDoc(md.Node))
	switch n := md.Node.(type) {
	case *ast.CommandDecl:
		e.access(n.Access, md.Public())
		e.body(n.Body)
		e.callers(d, md)
		e.places(md)
	case *ast.QueryDecl:
		e.access(n.Access, md.Public())
		e.body(n.Body)
		e.places(md)
	case *ast.EventDecl:
		e.event(d, md, n)
	case *ast.RuleDecl:
		e.body(n.Body)
		e.usedIn(d, md)
	case *ast.MethodDecl:
		e.body(n.Body)
	default:
		e.usedIn(d, md)
		for impl, decl := range e.info.Impls {
			if decl == md {
				e.line(0, "%s:", e.t(lblFake))
				e.body(impl.Body)
			}
		}
	}
}

func (e *explainer) access(l *ast.AccessList, public bool) {
	if !public {
		e.line(0, "%s: %s", e.t(lblAccess), e.t(lblNoAccess))
		return
	}
	if l == nil {
		e.line(0, "%s: %s", e.t(lblAccess), e.t(lblAnyone))
		return
	}
	var parts []string
	for _, id := range l.Names {
		a := e.info.Access[id]
		switch a.Kind {
		case sema.AccessSelf:
			parts = append(parts, "self")
		case sema.AccessField:
			parts = append(parts, e.t("the actor in field %s", a.Field.Name))
		case sema.AccessActor:
			parts = append(parts, e.t("actor %s", a.Actor.Name))
		case sema.AccessRole:
			parts = append(parts, e.t("role %s", a.Role.Name.Name))
		}
	}
	e.line(0, "%s: %s", e.t(lblAccess), strings.Join(parts, ", "))
}

// body describes the checks (branches with errors or an exit) and the actions.
func (e *explainer) body(b *ast.Block) {
	var checks []string
	guarded := map[ast.Node]bool{} // errors described in checks
	ast.Inspect(b, func(n ast.Node) bool {
		s, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		var errs []string
		exits := false
		var result ast.Expr
		for _, st := range s.Body.List {
			switch st := st.(type) {
			case *ast.ReturnStmt:
				exits, result = true, st.Result
			case *ast.ExprStmt:
				if x, ok := st.X.(*ast.CallExpr); ok {
					if c := e.info.Calls[x]; c != nil && c.Kind == sema.CallMethod && c.Method.Kind == types.Error {
						errs = append(errs, e.src(x))
						guarded[x] = true
					}
				}
			}
		}
		cond := e.t(lblIf, e.src(s.Cond))
		switch {
		case result != nil && len(s.Body.List) == 1:
			checks = append(checks, cond+" → "+e.t(lblReturns, e.src(result)))
		case len(errs) > 0 && exits:
			checks = append(checks, cond+" → "+e.t(lblErrorsExit, strings.Join(errs, ", ")))
		case len(errs) > 0:
			checks = append(checks, cond+" → "+e.t(lblErrors, strings.Join(errs, ", ")))
		case exits && len(s.Body.List) == 1:
			what := e.t(lblExitNoErr)
			if isHasErrors(e.info, s.Cond) {
				what = e.t(lblExit)
			}
			checks = append(checks, cond+" → "+what)
		}
		return true
	})
	if len(checks) > 0 {
		e.line(0, "%s:", e.t(lblChecks))
		for _, c := range checks {
			e.line(1, "%s", c)
		}
	}
	var acts []string
	for _, ef := range Effects(e.info, b) {
		if ef.Kind == EffError && guarded[ef.Node] {
			continue
		}
		if a := e.action(ef); a != "" {
			acts = append(acts, a)
		}
	}
	if len(acts) > 0 {
		e.line(0, "%s:", e.t(lblActions))
		for _, a := range acts {
			e.line(1, "%s", a)
		}
	}
	e.flags(b)
}

// flags describes the actions in branches of conditions with feature flags (RFC §23.4).
func (e *explainer) flags(b *ast.Block) {
	var out []string
	ast.Inspect(b, func(n ast.Node) bool {
		s, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		flags := e.info.FeaturesIn(s.Cond)
		if len(flags) == 0 {
			return true
		}
		f := flags[0]
		out = append(out, e.t(lblFlagOn, f.Name, f.Feature.Date.Value)+": "+e.actions(s.Body))
		if s.Else != nil {
			out = append(out, e.t(lblFlagOff, f.Name)+": "+e.actions(s.Else))
		}
		return true
	})
	if len(out) > 0 {
		e.line(0, "%s:", e.t(lblFlags))
		for _, l := range out {
			e.line(1, "%s", l)
		}
	}
}

// actions lists the actions of a node separated by semicolons.
func (e *explainer) actions(n ast.Node) string {
	var acts []string
	for _, ef := range Effects(e.info, n) {
		if a := e.action(ef); a != "" {
			acts = append(acts, a)
		}
	}
	if len(acts) == 0 {
		return e.t(lblNoActions)
	}
	return strings.Join(acts, "; ")
}

func isHasErrors(info *sema.Info, x ast.Expr) bool {
	id, ok := x.(*ast.Ident)
	if !ok {
		return false
	}
	o := info.Uses[id]
	return o != nil && o.Kind == sema.ObjBuiltin && o.Name == "hasErrors"
}

func (e *explainer) action(ef Effect) string {
	switch ef.Kind {
	case EffEvent:
		s := e.t(actEvent, ef.Method.Name)
		if f := eventFields(ef.Method); len(f) > 0 {
			s += ": " + strings.Join(f, ", ")
		}
		return s
	case EffCommand:
		if ef.Target == nil {
			return e.t(actCommand, ef.Method.Name)
		}
		s := e.t(actOnTarget, ef.Name(), e.src(ef.Target))
		if ef.Safe {
			s += e.t(actIfExists)
		}
		return s
	case EffNotify:
		if ef.Target != nil {
			return e.t(actNotifyTo, ef.Method.Name, e.src(ef.Target))
		}
		return e.t(actNotify, ef.Method.Name)
	case EffError:
		return e.t(actError, e.src(ef.Node))
	case EffRule:
		return e.t(actRule, ef.Method.Name)
	case EffQuery:
		return e.t(actQuery, ef.Name())
	case EffRequest:
		return e.t(actRequest, ef.Name())
	case EffUpsert:
		return e.t(actUpsert, ef.Decl.Name)
	case EffProjDelete:
		return e.t(actDelete, ef.Decl.Name)
	case EffSchedule:
		return e.t(actSchedule, ef.Method.Name)
	case EffTimerDelete:
		return e.t(actTimerDel, e.src(ef.Target))
	case EffFakeCall:
		return e.t(actFakeCall, ef.Decl.Name+"."+ef.Method.Name, e.src(ef.Target))
	case EffForget:
		return e.t(actForget, e.src(ef.Target))
	}
	return ""
}

// eventFields are the fields an event changes.
func eventFields(m *types.Method) []string {
	ev, ok := m.Node.(*ast.EventDecl)
	if !ok || ev.Body == nil {
		return nil
	}
	var out []string
	for _, s := range ev.Body.List {
		if a, ok := s.(*ast.AssignStmt); ok {
			if id, ok := a.Target.(*ast.Ident); ok && !contains(out, id.Name) {
				out = append(out, id.Name)
			}
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (e *explainer) event(d *types.Decl, md *types.Method, n *ast.EventDecl) {
	if f := eventFields(md); len(f) > 0 {
		e.line(0, "%s: %s", e.t(lblChanges), strings.Join(f, ", "))
	}
	var by []string
	var reactions []string
	for _, m := range Members(e.info) {
		if on, ok := m.Node.(*ast.OnDecl); ok && m.Decl == d && on.Event.Name == md.Name {
			reactions = append(reactions, m.Name())
		}
		for _, ef := range Effects(e.info, m.Body) {
			if ef.Kind == EffEvent && ef.Method == md && !contains(by, m.Name()) {
				by = append(by, m.Name())
			}
		}
	}
	if len(by) > 0 {
		e.line(0, "%s: %s", e.t(lblRaisedBy), strings.Join(by, ", "))
	}
	if len(reactions) > 0 {
		e.line(0, "%s: %s", e.t(lblReactions), strings.Join(reactions, ", "))
	}
}

// callers lists who calls a command: other commands, reactions, timers.
func (e *explainer) callers(d *types.Decl, md *types.Method) {
	var out []string
	for _, m := range Members(e.info) {
		for _, ef := range Effects(e.info, m.Body) {
			var s string
			switch {
			case ef.Kind == EffCommand && ef.Method == md:
				s = e.t(fromCommand, m.Name())
			case ef.Kind == EffSchedule && ef.Method == md:
				s = e.t(fromTimer, m.Name())
			}
			if s != "" && !contains(out, s) {
				out = append(out, s)
			}
		}
	}
	for _, d := range e.m.Program.Decls {
		if mg, ok := d.(*ast.MigrationDecl); ok {
			for _, ef := range Effects(e.info, mg.Body) {
				if ef.Kind == EffCommand && ef.Method == md {
					s := e.t("from migration %s", mg.Name.Name)
					if !contains(out, s) {
						out = append(out, s)
					}
				}
			}
		}
	}
	if len(out) > 0 {
		e.line(0, "%s:", e.t(lblCalledFrom))
		for _, s := range out {
			e.line(1, "%s", s)
		}
	}
}

// places are the UI places with this method (RFC §22.10).
func (e *explainer) places(md *types.Method) {
	var out []string
	if u := e.info.UI; u != nil {
		for _, a := range u.Apps {
			for _, p := range a.Places {
				if p.Node.Method == md {
					out = append(out, p.Name)
				}
			}
		}
	}
	if len(out) > 0 {
		e.line(0, "%s: %s", e.t(lblPlaces), strings.Join(out, ", "))
	}
}

// usedIn lists where a notification, error, rule or query is called.
func (e *explainer) usedIn(d *types.Decl, md *types.Method) {
	var out []string
	for _, m := range Members(e.info) {
		for _, ef := range Effects(e.info, m.Body) {
			if ef.Method == md && !contains(out, m.Name()) {
				out = append(out, m.Name())
			}
		}
	}
	if len(out) > 0 {
		e.line(0, "%s: %s", e.t(lblUsedIn), strings.Join(out, ", "))
	}
}

func declDoc(n ast.Decl) *ast.CommentGroup {
	switch n := n.(type) {
	case *ast.EntityDecl:
		return n.Doc
	case *ast.ProjectionDecl:
		return n.Doc
	case *ast.ValueDecl:
		return n.Doc
	case *ast.EnumDecl:
		return n.Doc
	case *ast.ExternalDecl:
		return n.Doc
	case *ast.AppDecl:
		return n.Doc
	case *ast.ScreenDecl:
		return n.Doc
	case *ast.PrimitiveDecl:
		return n.Doc
	}
	return nil
}

func memberDoc(n ast.Member) *ast.CommentGroup {
	switch n := n.(type) {
	case *ast.CommandDecl:
		return n.Doc
	case *ast.EventDecl:
		return n.Doc
	case *ast.QueryDecl:
		return n.Doc
	case *ast.RuleDecl:
		return n.Doc
	case *ast.MethodDecl:
		return n.Doc
	case *ast.NotificationDecl:
		return n.Doc
	case *ast.ErrorDecl:
		return n.Doc
	case *ast.RequestDecl:
		return n.Doc
	}
	return nil
}
