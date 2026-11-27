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

// NodeKind is the kind of a diagram node (colors as on an Event Storming board).
type NodeKind uint8

const (
	NodeCommand    NodeKind = iota // a private command
	NodePublic                     // a public command
	NodeEvent                      // an event
	NodeReaction                   // an on reaction of an extension
	NodeRequest                    // a request to an external system
	NodeProjection                 // a projection
	NodeApp                        // an app: the root of its layout
	NodeScreen                     // a screen
	NodeContext                    // a bounded context (context map)
)

// EdgeKind is the kind of an edge.
type EdgeKind uint8

const (
	EdgeRaises  EdgeKind = iota // a command emits an event
	EdgeCalls                   // a command calls a command
	EdgeTimer                   // a command sets a timer for a command
	EdgeRequest                 // a command sends a request to an external system
	EdgeUpdates                 // a command updates or deletes a projection row
	EdgeReacts                  // an event triggers a reaction
	EdgeAnswers                 // a system fake answers with a model command
	EdgeOpens                   // a transition to a screen: menu, Link, on, embedding
)

// Graph is the diagram "commands → events → calls → timers → external systems".
type Graph struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

// GraphNode is a node: a command, event, reaction, request, projection.
type GraphNode struct {
	ID   string // "Order.place", "Order.on placed"
	Kind NodeKind
}

// GraphEdge is an edge between nodes.
type GraphEdge struct {
	From, To string
	Kind     EdgeKind
	Label    string // EdgeOpens: the transition event; others: the feature flag of the branch ("tips", "not tips")
}

// BuildGraph builds the diagram; entity != "" keeps only the edges of the
// commands and reactions of that type.
func BuildGraph(info *sema.Info, entity string) *Graph {
	g := &Graph{}
	nodes := map[string]NodeKind{}
	edges := map[GraphEdge]bool{}
	node := func(id string, k NodeKind) {
		if _, ok := nodes[id]; !ok {
			nodes[id] = k
		}
	}
	var flag func(n ast.Node) string
	edge := func(from, to string, k EdgeKind, n ast.Node) {
		edges[GraphEdge{From: from, To: to, Kind: k, Label: flag(n)}] = true
	}
	var only *types.Decl
	if entity != "" {
		only = info.Lookup(entity)
	}
	for _, m := range Members(info) {
		if only != nil && m.Decl != only {
			continue
		}
		var from string
		switch n := m.Node.(type) {
		case *ast.CommandDecl:
			from = m.Decl.Name + "." + n.Name.Name
			if md := m.Decl.Method(n.Name.Name); md != nil && md.Public() {
				node(from, NodePublic)
			} else {
				node(from, NodeCommand)
			}
		case *ast.OnDecl:
			from = m.Name()
			node(from, NodeReaction)
			ev := m.Decl.Name + "." + n.Event.Name
			node(ev, NodeEvent)
			edges[GraphEdge{From: ev, To: from, Kind: EdgeReacts}] = true
		case *ast.RequestDecl:
			from = m.Name() // a request implementation in a fake
			node(from, NodeRequest)
		default:
			continue
		}
		flag = FlagBranches(info, m.Body)
		for _, e := range Effects(info, m.Body) {
			switch e.Kind {
			case EffEvent:
				node(e.Name(), NodeEvent)
				edge(from, e.Name(), EdgeRaises, e.Node)
			case EffCommand:
				node(e.Name(), commandKind(e.Method))
				edge(from, e.Name(), EdgeCalls, e.Node)
			case EffSchedule:
				node(e.Name(), commandKind(e.Method))
				edge(from, e.Name(), EdgeTimer, e.Node)
			case EffRequest:
				node(e.Name(), NodeRequest)
				edge(from, e.Name(), EdgeRequest, e.Node)
			case EffUpsert, EffProjDelete:
				node(e.Name(), NodeProjection)
				edge(from, e.Name(), EdgeUpdates, e.Node)
			case EffFakeCall:
				node(e.Name(), commandKind(e.Method))
				edge(from, e.Name(), EdgeAnswers, e.Node)
			}
		}
	}
	for e := range edges {
		g.Edges = append(g.Edges, e)
	}
	sort.Slice(g.Edges, func(i, j int) bool {
		a, b := g.Edges[i], g.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Kind < b.Kind
	})
	// With a type filter, the nodes that have edges remain.
	used := map[string]bool{}
	for _, e := range g.Edges {
		used[e.From], used[e.To] = true, true
	}
	for id, k := range nodes {
		if entity == "" || used[id] {
			g.Nodes = append(g.Nodes, GraphNode{ID: id, Kind: k})
		}
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	return g
}

// FlagBranches returns a function that names the feature flag of the branch
// a body node stands in: "tips" for the "on" branch, "not tips" for else;
// "" when the node is not under a flag.
func FlagBranches(info *sema.Info, body ast.Node) func(ast.Node) string {
	type branch struct {
		from, to int
		label    string
	}
	var bs []branch
	if body != nil {
		ast.Inspect(body, func(n ast.Node) bool {
			s, ok := n.(*ast.IfStmt)
			if !ok {
				return true
			}
			if fs := info.FeaturesIn(s.Cond); len(fs) > 0 {
				name := fs[0].Feature.Name.Name
				bs = append(bs, branch{int(s.Body.Pos()), int(s.Body.End()), name})
				if s.Else != nil {
					bs = append(bs, branch{int(s.Else.Pos()), int(s.Else.End()), "not " + name})
				}
			}
			return true
		})
	}
	return func(n ast.Node) string {
		if n == nil {
			return ""
		}
		for _, b := range bs {
			if p := int(n.Pos()); p >= b.from && p < b.to {
				return b.label
			}
		}
		return ""
	}
}

func commandKind(m *types.Method) NodeKind {
	if m.Public() {
		return NodePublic
	}
	return NodeCommand
}

// nodeID is the node identifier for mermaid and dot.
func nodeID(id string) string {
	return strings.NewReplacer(".", "_", " ", "_", ":", "").Replace(id)
}

// WriteMermaid prints the diagram in Mermaid format (flowchart).
func (g *Graph) WriteMermaid(w io.Writer) error {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for _, n := range g.Nodes {
		open, close := "[", "]"
		switch n.Kind {
		case NodeEvent:
			open, close = "([", "])"
		case NodeReaction:
			open, close = "{{", "}}"
		case NodeRequest:
			open, close = "[/", "/]"
		case NodeProjection:
			open, close = "[(", ")]"
		case NodeApp:
			open, close = "((", "))"
		}
		fmt.Fprintf(&b, "  %s%s\"%s\"%s:::%s\n", nodeID(n.ID), open, n.ID, close, mermaidClass[n.Kind])
	}
	for _, e := range g.Edges {
		arrow := "-->"
		switch e.Kind {
		case EdgeTimer:
			arrow = "-. timer .->"
		case EdgeReacts:
			arrow = "-.->"
		case EdgeAnswers:
			arrow = "-. fake .->"
		default:
			if e.Label != "" {
				arrow = "-- " + e.Label + " -->"
			}
		}
		fmt.Fprintf(&b, "  %s %s %s\n", nodeID(e.From), arrow, nodeID(e.To))
	}
	b.WriteString("  classDef command fill:#a7c7e7,stroke:#4a6f94\n")
	b.WriteString("  classDef public fill:#a7c7e7,stroke:#1f3f66,stroke-width:3px\n")
	b.WriteString("  classDef event fill:#ffb347,stroke:#b36b00\n")
	b.WriteString("  classDef reaction fill:#c8a2c8,stroke:#7a4f7a\n")
	b.WriteString("  classDef request fill:#f4a6c6,stroke:#a3456e\n")
	b.WriteString("  classDef projection fill:#b5e7a0,stroke:#4f8a3a\n")
	b.WriteString("  classDef app fill:#fff3b0,stroke:#9c8a1f\n")
	b.WriteString("  classDef screen fill:#e8e8e8,stroke:#666666\n")
	b.WriteString("  classDef context fill:#fdfdfd,stroke:#333333,stroke-width:2px\n")
	_, err := io.WriteString(w, b.String())
	return err
}

var mermaidClass = map[NodeKind]string{
	NodeCommand: "command", NodePublic: "public", NodeEvent: "event",
	NodeReaction: "reaction", NodeRequest: "request", NodeProjection: "projection",
	NodeApp: "app", NodeScreen: "screen", NodeContext: "context",
}

// WriteDot prints the diagram in Graphviz DOT format.
func (g *Graph) WriteDot(w io.Writer) error {
	var b strings.Builder
	b.WriteString("digraph model {\n  rankdir=LR;\n  node [style=filled, fontname=\"Helvetica\"];\n")
	for _, n := range g.Nodes {
		fmt.Fprintf(&b, "  %s [label=%q, %s];\n", nodeID(n.ID), n.ID, dotStyle[n.Kind])
	}
	for _, e := range g.Edges {
		style := ""
		switch e.Kind {
		case EdgeTimer:
			style = " [style=dashed, label=\"timer\"]"
		case EdgeReacts:
			style = " [style=dotted]"
		case EdgeAnswers:
			style = " [style=dashed, label=\"fake\"]"
		default:
			if e.Label != "" {
				style = fmt.Sprintf(" [label=%q]", e.Label)
			}
		}
		fmt.Fprintf(&b, "  %s -> %s%s;\n", nodeID(e.From), nodeID(e.To), style)
	}
	b.WriteString("}\n")
	_, err := io.WriteString(w, b.String())
	return err
}

var dotStyle = map[NodeKind]string{
	NodeCommand:    `shape=box, fillcolor="#a7c7e7"`,
	NodePublic:     `shape=box, fillcolor="#a7c7e7", penwidth=3`,
	NodeEvent:      `shape=box, style="rounded,filled", fillcolor="#ffb347"`,
	NodeReaction:   `shape=hexagon, fillcolor="#c8a2c8"`,
	NodeRequest:    `shape=parallelogram, fillcolor="#f4a6c6"`,
	NodeProjection: `shape=cylinder, fillcolor="#b5e7a0"`,
	NodeApp:        `shape=doublecircle, fillcolor="#fff3b0"`,
	NodeScreen:     `shape=box, style="rounded,filled", fillcolor="#e8e8e8"`,
	NodeContext:    `shape=box3d, fillcolor="#fdfdfd"`,
}

// BuildContextMap builds the context map (docs/plans/0011, B5): nodes are
// bounded contexts, edges are ACLs from the source to the receiver; the
// label lists the events the ACL reacts to and the actors it translates.
func BuildContextMap(info *sema.Info) *Graph {
	g := &Graph{}
	nodes := map[string]bool{}
	for _, d := range info.DeclList {
		if d.Context != "" {
			nodes[d.Context] = true
		}
	}
	for _, a := range info.ACLs {
		var what []string
		for _, m := range a.Node.Members {
			switch m := m.(type) {
			case *ast.OnDecl:
				if t := info.On[m]; t != nil {
					what = append(what, t.Model.Short+"."+m.Event.Name)
				}
			case *ast.ActorAsDecl:
				if m.To != nil {
					what = append(what, "actor "+m.To.Name)
				}
			}
		}
		g.Edges = append(g.Edges, GraphEdge{From: a.Source, To: a.Context, Kind: EdgeCalls, Label: strings.Join(what, ", ")})
	}
	for n := range nodes {
		g.Nodes = append(g.Nodes, GraphNode{ID: n, Kind: NodeContext})
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.Slice(g.Edges, func(i, j int) bool {
		return g.Edges[i].From+g.Edges[i].To < g.Edges[j].From+g.Edges[j].To
	})
	return g
}

// BuildScreenGraph builds the transitions between the screens of an app:
// menu, Link, embeddings and on transitions (labeled with the event).
func BuildScreenGraph(info *sema.Info, app string) *Graph {
	g := &Graph{}
	a := info.UI.App(app)
	if a == nil {
		return g
	}
	g.Nodes = append(g.Nodes, GraphNode{ID: a.Name, Kind: NodeApp})
	for _, s := range a.Screens {
		g.Nodes = append(g.Nodes, GraphNode{ID: s.Name, Kind: NodeScreen})
	}
	seen := map[GraphEdge]bool{}
	var walk func(from string, nodes []*sema.Node)
	walk = func(from string, nodes []*sema.Node) {
		for _, n := range nodes {
			if n.To != nil {
				e := GraphEdge{From: from, To: n.To.Name, Kind: EdgeOpens}
				if n.Kind == sema.NodeOn {
					e.Label = n.Event.Name
				}
				if !seen[e] {
					seen[e] = true
					g.Edges = append(g.Edges, e)
				}
			}
			walk(from, n.Children)
		}
	}
	walk(a.Name, a.Nodes)
	for _, s := range a.Screens {
		walk(s.Name, s.Nodes)
	}
	return g
}
