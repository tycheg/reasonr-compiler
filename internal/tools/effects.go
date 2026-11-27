// Package tools holds tools over a checked model: method description
// (reasonr explain), the command and event diagram (reasonr graph), the
// personal data map (reasonr pii). Plan: §16.
package tools

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// EffectKind is the kind of an action in a method body.
type EffectKind uint8

const (
	EffEvent       EffectKind = iota // an event of its own entity
	EffCommand                       // a command of its own or another entity
	EffNotify                        // a notification
	EffError                         // an error
	EffRule                          // a rule
	EffQuery                         // a query
	EffRequest                       // a request to an external system
	EffUpsert                        // a projection update
	EffProjDelete                    // a projection row deletion
	EffSchedule                      // a timer for a command
	EffTimerDelete                   // a timer deletion
	EffForget                        // forgetting a subject
	EffFakeCall                      // a fake calls a public model command through the API
)

// Effect is an action in a body: a call, an effect or a statement.
type Effect struct {
	Kind   EffectKind
	Decl   *types.Decl   // the type of the method or the projection
	Method *types.Method // an event, command, notification, error, rule, query
	Node   ast.Node      // the call or the statement
	Target ast.Expr      // [x] for a command of another entity, to for a notification, the deleted value
	Safe   bool          // [x]?.command(…)
}

// Name returns "Type.method" or the type name.
func (e Effect) Name() string {
	if e.Method != nil {
		return e.Decl.Name + "." + e.Method.Name
	}
	if e.Decl != nil {
		return e.Decl.Name
	}
	return ""
}

// Effects returns the actions of a body in written order.
func Effects(info *sema.Info, body ast.Node) []Effect {
	var out []Effect
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.ExprStmt:
			if x, ok := s.X.(*ast.CallExpr); ok {
				if c := info.Calls[x]; c != nil && c.Kind == sema.CallMethod && c.Method.Kind == types.Notification {
					out = append(out, Effect{Kind: EffNotify, Decl: c.Decl, Method: c.Method, Node: x, Target: s.To})
					for _, a := range x.Args.Args {
						out = append(out, Effects(info, a.Value)...)
					}
					return false
				}
			}
		case *ast.DeleteStmt:
			if br, ok := s.X.(*ast.BracketExpr); ok && len(br.Elems) == 1 {
				if id, ok := types.Unwrap(info.Types[br.Elems[0]]).(*types.ID); ok {
					out = append(out, Effect{Kind: EffProjDelete, Decl: id.Owner, Node: s, Target: br.Elems[0]})
					return true
				}
			}
			out = append(out, Effect{Kind: EffTimerDelete, Node: s, Target: s.X})
		case *ast.ForgetStmt:
			var d *types.Decl
			if id, ok := types.Unwrap(info.Types[s.X]).(*types.ID); ok {
				d = id.Owner
			}
			out = append(out, Effect{Kind: EffForget, Decl: d, Node: s, Target: s.X})
		case *ast.CallExpr:
			c := info.Calls[s]
			if c == nil {
				return true
			}
			switch c.Kind {
			case sema.CallMethod:
				kind, ok := map[types.MethodKind]EffectKind{
					types.Command: EffCommand, types.Event: EffEvent, types.Notification: EffNotify,
					types.Error: EffError, types.Rule: EffRule,
				}[c.Method.Kind]
				if ok {
					out = append(out, Effect{Kind: kind, Decl: c.Decl, Method: c.Method, Node: s})
				}
			case sema.CallCommand:
				fun := s.Fun.(*ast.SelectorExpr)
				target := fun.X.(*ast.BracketExpr).Elems[0]
				out = append(out, Effect{Kind: EffCommand, Decl: c.Decl, Method: c.Method, Node: s, Target: target, Safe: fun.Safe})
			case sema.CallExternal:
				target := s.Fun.(*ast.SelectorExpr).X.(*ast.BracketExpr).Elems[0]
				out = append(out, Effect{Kind: EffFakeCall, Decl: c.Decl, Method: c.Method, Node: s, Target: target})
			case sema.CallQuery:
				out = append(out, Effect{Kind: EffQuery, Decl: c.Decl, Method: c.Method, Node: s})
			case sema.CallRequest:
				out = append(out, Effect{Kind: EffRequest, Decl: c.Decl, Method: c.Method, Node: s})
			case sema.CallUpsert:
				out = append(out, Effect{Kind: EffUpsert, Decl: c.Decl, Node: s})
			case sema.CallBuiltin:
				if c.Builtin != "schedule" {
					return true
				}
				// A timer command runs later, as a separate unit of work.
				for _, a := range s.Args.Args {
					run, ok := a.Value.(*ast.CallExpr)
					if a.Name == nil || a.Name.Name != "run" || !ok {
						out = append(out, Effects(info, a.Value)...)
						continue
					}
					if rc := info.Calls[run]; rc != nil && rc.Method != nil {
						out = append(out, Effect{Kind: EffSchedule, Decl: rc.Decl, Method: rc.Method, Node: s})
					}
					for _, ra := range run.Args.Args {
						out = append(out, Effects(info, ra.Value)...)
					}
				}
				return false
			}
		}
		return true
	})
	return out
}

// Member is a type member with a body.
type Member struct {
	Decl *types.Decl
	Node ast.Member
	Body *ast.Block
	// ACL, for an ACL reaction, is the bounded context that owns the ACL;
	// Decl is the entity of the source bounded context whose event it reacts to.
	ACL string
}

// Name returns "Type.name"; for a reaction "Type.on event", for an ACL
// reaction "Dispatch: Ordering.Order.on placed".
func (m Member) Name() string {
	switch n := m.Node.(type) {
	case *ast.OnDecl:
		if m.ACL != "" {
			return m.ACL + ": " + m.Decl.Name + ".on " + n.Event.Name
		}
		return m.Decl.Name + ".on " + n.Event.Name
	}
	if id := memberName(m.Node); id != nil {
		return m.Decl.Name + "." + id.Name
	}
	return m.Decl.Name
}

// Members returns the members with bodies in declaration order.
func Members(info *sema.Info) []Member {
	var out []Member
	for _, d := range info.DeclList {
		for _, m := range declMembers(d) {
			var b *ast.Block
			switch m := m.(type) {
			case *ast.CommandDecl:
				b = m.Body
			case *ast.EventDecl:
				b = m.Body
			case *ast.QueryDecl:
				b = m.Body
			case *ast.RuleDecl:
				b = m.Body
			case *ast.MethodDecl:
				b = m.Body
			case *ast.OnDecl:
				b = m.Body
			case *ast.RequestDecl:
				b = m.Body // an implementation in a fake
			}
			if b != nil {
				out = append(out, Member{Decl: d, Node: m, Body: b})
			}
		}
	}
	for _, a := range info.ACLs {
		for _, m := range a.Node.Members {
			if on, ok := m.(*ast.OnDecl); ok && info.On[on] != nil {
				out = append(out, Member{Decl: info.On[on].Model, Node: on, Body: on.Body, ACL: a.Context})
			}
		}
	}
	return out
}

func declMembers(d *types.Decl) []ast.Member {
	switch n := d.Node.(type) {
	case *ast.EntityDecl:
		return n.Members
	case *ast.ProjectionDecl:
		return n.Members
	case *ast.ValueDecl:
		return n.Members
	case *ast.ExternalDecl:
		return n.Members
	}
	return nil
}

func memberName(m ast.Member) *ast.Ident {
	switch n := m.(type) {
	case *ast.CommandDecl:
		return n.Name
	case *ast.EventDecl:
		return n.Name
	case *ast.QueryDecl:
		return n.Name
	case *ast.RuleDecl:
		return n.Name
	case *ast.MethodDecl:
		return n.Name
	case *ast.NotificationDecl:
		return n.Name
	case *ast.ErrorDecl:
		return n.Name
	case *ast.RequestDecl:
		return n.Name
	}
	return nil
}
