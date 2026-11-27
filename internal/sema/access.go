package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
	"strings"
)

var noteRole = "the role is declared here"

// resolveAccess resolves the access lists of commands, queries and apps
// (RFC §28): self, a field, an actor, a role.
func (c *checker) resolveAccess(d *types.Decl) {
	switch n := d.Node.(type) {
	case *ast.EntityDecl:
		c.memberAccess(d, n.Members)
	case *ast.ProjectionDecl:
		c.memberAccess(d, n.Members)
	case *ast.AppDecl:
		c.accessList(nil, n.Access, false)
	case *ast.ExternalDecl:
		c.systemRole(d, n.Access)
	}
}

// systemAnswers warns about an external system with a role that no public
// command is open to: it has nothing to answer with (RFC §18).
func (c *checker) systemAnswers() {
	for _, d := range c.info.DeclList {
		if d.Kind != types.ExternalDecl || d.Role == "" {
			continue
		}
		if !c.roleAnswers(d.Role) {
			n := d.Node.(*ast.ExternalDecl)
			c.bag.Add(diag.W1030, rangeOf(n.Access), d.Name, d.Role)
		}
	}
}

// roleAnswers reports whether at least one public command is open to the
// role: without an access list or with the role in it.
func (c *checker) roleAnswers(role string) bool {
	for _, d := range c.info.DeclList {
		for _, m := range d.Methods {
			if m.Kind != types.Command || !m.Public() {
				continue
			}
			cmd, ok := m.Node.(*ast.CommandDecl)
			if !ok {
				continue
			}
			if cmd.Access == nil {
				return true
			}
			for _, id := range cmd.Access.Names {
				if a, ok := c.info.Access[id]; ok && a.Kind == AccessRole && c.info.RoleNames[a.Role] == role {
					return true
				}
			}
		}
	}
	return false
}

// systemRole resolves the role of an external system: external(role) is
// exactly one role declared with role.
func (c *checker) systemRole(d *types.Decl, l *ast.AccessList) {
	if l == nil {
		return
	}
	for i, id := range l.Names {
		if id.Name == "" {
			continue
		}
		role := c.role(id, id.Name)
		if role == nil || i > 0 {
			diagnostic := c.bag.Add(diag.E1201, rangeOf(id), d.Name, id.Name)
			if role == nil {
				var roles []string
				for r := range c.info.Roles {
					roles = append(roles, r)
				}
				if s := closest(id.Name, roles); s != "" {
					diagnostic.Hint(hintDidYouMean, s)
				}
			}
			continue
		}
		c.info.Access[id] = Access{Kind: AccessRole, Role: role}
		d.Role = c.info.RoleNames[role]
	}
}

func (c *checker) memberAccess(d *types.Decl, members []ast.Member) {
	for _, m := range members {
		switch m := m.(type) {
		case *ast.CommandDecl:
			c.accessList(d, m.Access, true)
		case *ast.QueryDecl:
			c.accessList(d, m.Access, false)
		}
	}
}

// accessList resolves the access list of a method of type owner (nil for an
// app); command: it is a command, the only kind that has self and fields.
func (c *checker) accessList(owner *types.Decl, l *ast.AccessList, command bool) {
	if l == nil {
		return
	}
	for _, id := range l.Names {
		if id.Name == "" {
			continue
		}
		if a, ok := c.accessName(owner, id, command); ok {
			c.info.Access[id] = a
		}
	}
}

func (c *checker) accessName(owner *types.Decl, id *ast.Ident, command bool) (Access, bool) {
	name := id.Name
	role := c.role(id, name)
	if name == "self" {
		if !command || owner.Kind != types.ActorDecl {
			c.bag.Add(diag.E0601, rangeOf(id))
			return Access{}, false
		}
		return Access{Kind: AccessSelf}, true
	}
	var field *types.Var
	if owner != nil {
		field = owner.Field(name)
	}
	if field != nil {
		fieldName := field.Node.(*ast.FieldDecl).Name
		switch {
		case !command:
			c.bag.Add(diag.E0606, rangeOf(id), name).Note(rangeOf(fieldName), noteDeclared)
			return Access{}, false
		case role != nil:
			c.bag.Add(diag.E0603, rangeOf(id), name).
				Note(rangeOf(fieldName), noteDeclared).
				Note(rangeOf(role.Name), noteRole)
			return Access{}, false
		}
		if t, ok := types.Unwrap(field.Type).(*types.ID); !ok || t.Owner.Kind != types.ActorDecl {
			if !types.IsInvalid(field.Type) {
				c.bag.Add(diag.E0602, rangeOf(id), name, field.Type.String()).Note(rangeOf(fieldName), noteDeclared)
			}
			return Access{}, false
		}
		return Access{Kind: AccessField, Field: field}, true
	}
	if d := c.lookup(id, strings.Split(name, ".")...); d != nil && d.Kind == types.ActorDecl {
		return Access{Kind: AccessActor, Actor: d}, true
	}
	if role != nil {
		return Access{Kind: AccessRole, Role: role}, true
	}
	diagnostic := c.bag.Add(diag.E0604, rangeOf(id), name)
	if s := closest(name, c.accessCandidates(owner, command)); s != "" {
		diagnostic.Hint(hintDidYouMean, s)
	}
	return Access{}, false
}

func (c *checker) accessCandidates(owner *types.Decl, command bool) []string {
	var out []string
	for r := range c.info.Roles {
		out = append(out, r)
	}
	for _, d := range c.info.DeclList {
		if d.Kind == types.ActorDecl {
			out = append(out, d.Name)
		}
	}
	if command && owner != nil {
		for _, f := range owner.Fields {
			out = append(out, f.Name)
		}
		if owner.Kind == types.ActorDecl {
			out = append(out, "self")
		}
	}
	return out
}
