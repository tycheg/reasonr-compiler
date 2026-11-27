package tools

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Analysis of extracting a bounded context into a separate process
// (docs/plans/0011, §3.3, B6). While bounded contexts share a process, an
// ACL reaction runs in the same unit of work as the source command. After
// the extraction it runs right after, as its own unit of work: a reaction
// error no longer cancels the source command, and another command may run
// between them (a race).

// Crossing is an ACL reaction across the border of the extracted bounded context.
type Crossing struct {
	ACL      string   // the ACL's bounded context, the receiver
	Source   string   // the bounded context of the event source
	Event    string   // "Ordering.Order.placed"
	Commands []string // the receiver's commands the reaction calls (nested ones included)
	Errors   []string // the errors these commands may raise
}

// Extraction is the analysis: reactions into the extracted bounded context and out of it.
type Extraction struct {
	Context  string
	Incoming []Crossing // ACLs of the extracted bounded context on events of others
	Outgoing []Crossing // ACLs of other bounded contexts on its events
	Actors   []string   // actor translations across the border
}

// Extract analyzes the extraction of bounded context ctx.
func Extract(info *sema.Info, ctx string) *Extraction {
	x := &Extraction{Context: ctx}
	for _, a := range info.ACLs {
		if a.Context != ctx && a.Source != ctx {
			continue
		}
		for _, m := range a.Node.Members {
			switch m := m.(type) {
			case *ast.OnDecl:
				t := info.On[m]
				if t == nil {
					continue
				}
				c := Crossing{ACL: a.Context, Source: a.Source, Event: t.Model.Name + "." + t.Event.Name}
				c.Commands, c.Errors = reach(info, m.Body)
				if a.Context == ctx {
					x.Incoming = append(x.Incoming, c)
				} else {
					x.Outgoing = append(x.Outgoing, c)
				}
			case *ast.ActorAsDecl:
				if m.To != nil {
					x.Actors = append(x.Actors, fmt.Sprintf("%s: actor %s as %s", a.Context, names2(m.From), m.To.Name))
				}
			}
		}
	}
	return x
}

func names2(ids []*ast.Ident) string {
	var s []string
	for _, id := range ids {
		s = append(s, id.Name)
	}
	return strings.Join(s, ".")
}

// reach returns the commands a body calls (with nested calls) and the
// errors they may raise, by name, without repeats.
func reach(info *sema.Info, body ast.Node) (cmds, errs []string) {
	seenCmd, seenErr := map[string]bool{}, map[string]bool{}
	bodies := map[*types.Method]*ast.Block{}
	for _, mb := range Members(info) {
		if c, ok := mb.Node.(*ast.CommandDecl); ok {
			if m := mb.Decl.Method(c.Name.Name); m != nil {
				bodies[m] = mb.Body
			}
		}
	}
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		for _, e := range Effects(info, n) {
			switch e.Kind {
			case EffCommand:
				name := e.Name()
				if seenCmd[name] {
					continue
				}
				seenCmd[name] = true
				cmds = append(cmds, name)
				if b := bodies[e.Method]; b != nil {
					walk(b)
				}
			case EffError:
				if !seenErr[e.Name()] {
					seenErr[e.Name()] = true
					errs = append(errs, e.Name())
				}
			}
		}
	}
	walk(body)
	sort.Strings(errs)
	return cmds, errs
}

// WriteExtraction prints the analysis as text.
func WriteExtraction(w io.Writer, x *Extraction) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Extracting bounded context %s: ACL reactions become separate units of work\n", x.Context)
	fmt.Fprintf(&b, "(at-least-once delivery, right after the source command). Check each one:\n")
	fmt.Fprintf(&b, "a reaction error no longer cancels the source command, and another command may run between them.\n")
	section := func(title string, cs []Crossing) {
		fmt.Fprintf(&b, "\n%s:\n", title)
		if len(cs) == 0 {
			b.WriteString("  none\n")
		}
		for _, c := range cs {
			fmt.Fprintf(&b, "  %s ← %s\n", c.ACL, c.Event)
			if len(c.Commands) > 0 {
				fmt.Fprintf(&b, "    commands: %s\n", strings.Join(c.Commands, ", "))
			}
			if len(c.Errors) > 0 {
				fmt.Fprintf(&b, "    errors that no longer cancel the source command: %s\n", strings.Join(c.Errors, ", "))
			}
		}
	}
	section("Into the bounded context (its ACLs on events of others)", x.Incoming)
	section("Out of the bounded context (ACLs of others on its events)", x.Outgoing)
	if len(x.Actors) > 0 {
		b.WriteString("\nActor translations (synchronous: checked on every call):\n")
		for _, a := range x.Actors {
			fmt.Fprintf(&b, "  %s\n", a)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
