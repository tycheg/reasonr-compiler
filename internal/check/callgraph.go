package check

import (
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Graph is the command call graph (plan, §12.1). on reactions are nodes too:
// a command that emits an event leads to the reactions to it.
type Graph struct {
	Nodes []*Node
	byKey map[any]*Node
}

// Node is a command or an on reaction.
type Node struct {
	Decl   *types.Decl
	Method *types.Method // the command; nil for a reaction
	On     *ast.OnDecl   // the reaction
	Edges  []*Node
}

// Name is "Order.place" or "Order.on placed".
func (n *Node) Name() string {
	if n.On != nil {
		return n.Decl.Name + ".on " + n.On.Event.Name
	}
	return n.Decl.Name + "." + n.Method.Name
}

func (n *Node) ident() *ast.Ident {
	if n.On != nil {
		return n.On.Event
	}
	return n.Method.Node.(*ast.CommandDecl).Name
}

// CallGraph builds the call graph and looks for recursion (E0401).
var CallGraph = &Analyzer{
	Name:  "callgraph",
	Doc:   "command call graph; command recursion is not allowed",
	Codes: []diag.Code{"E0401"},
	Run:   runCallGraph,
}

func runCallGraph(pass *Pass) any {
	g := buildGraph(pass.Info)
	for _, scc := range g.cycles() {
		first := scc[0]
		names := make([]string, len(scc)+1)
		for i, n := range scc {
			names[i] = n.Name()
		}
		names[len(scc)] = first.Name()
		pass.Report(diag.E0401, rangeOf(first.ident()), strings.Join(names, " → "))
	}
	return g
}

func buildGraph(info *sema.Info) *Graph {
	g := &Graph{byKey: map[any]*Node{}}
	node := func(key any, mk func() *Node) *Node {
		if n := g.byKey[key]; n != nil {
			return n
		}
		n := mk()
		g.byKey[key] = n
		g.Nodes = append(g.Nodes, n)
		return n
	}
	// Reactions by event: the owner and the event name.
	type evKey struct {
		d    *types.Decl
		name string
	}
	reactions := map[evKey][]*Node{}
	members := bodies(info)
	for _, m := range members {
		switch n := m.node.(type) {
		case *ast.CommandDecl:
			if md := m.decl.Method(n.Name.Name); md != nil && md.Node == n {
				node(md, func() *Node { return &Node{Decl: m.decl, Method: md} })
			}
		case *ast.OnDecl:
			r := node(n, func() *Node { return &Node{Decl: m.decl, On: n} })
			reactions[evKey{m.decl, n.Event.Name}] = append(reactions[evKey{m.decl, n.Event.Name}], r)
		}
	}
	for _, m := range members {
		var from *Node
		switch n := m.node.(type) {
		case *ast.CommandDecl:
			from = g.byKey[m.decl.Method(n.Name.Name)]
		case *ast.OnDecl:
			from = g.byKey[n]
		}
		if from == nil {
			continue
		}
		for _, call := range calls(info, m.body) {
			c := info.Calls[call]
			switch {
			case c.Kind == sema.CallCommand, c.Kind == sema.CallMethod && c.Method.Kind == types.Command:
				if to := g.byKey[c.Method]; to != nil {
					from.Edges = append(from.Edges, to)
				}
			case c.Kind == sema.CallMethod && c.Method.Kind == types.Event:
				from.Edges = append(from.Edges, reactions[evKey{c.Decl, c.Method.Name}]...)
			}
		}
	}
	return g
}

// cycles returns strongly connected components of two or more nodes, or
// nodes with a self-loop (Tarjan); the nodes of a cycle are in visit order.
func (g *Graph) cycles() [][]*Node {
	index := map[*Node]int{}
	low := map[*Node]int{}
	onStack := map[*Node]bool{}
	var stack []*Node
	var out [][]*Node
	next := 0
	var strong func(v *Node)
	strong = func(v *Node) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range v.Edges {
			if _, seen := index[w]; !seen {
				strong(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] != index[v] {
			return
		}
		var scc []*Node
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			scc = append(scc, w)
			if w == v {
				break
			}
		}
		if len(scc) > 1 || hasEdge(v, v) {
			out = append(out, cyclePath(scc))
		}
	}
	for _, n := range g.Nodes {
		if _, seen := index[n]; !seen {
			strong(n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0].ident().Pos() < out[j][0].ident().Pos() })
	return out
}

func hasEdge(a, b *Node) bool {
	for _, e := range a.Edges {
		if e == b {
			return true
		}
	}
	return false
}

// cyclePath returns a cycle inside a component starting at the node declared first.
func cyclePath(scc []*Node) []*Node {
	in := map[*Node]bool{}
	start := scc[0]
	for _, n := range scc {
		in[n] = true
		if n.ident().Pos() < start.ident().Pos() {
			start = n
		}
	}
	// Breadth-first search from start back to start inside the component.
	prev := map[*Node]*Node{}
	queue := []*Node{start}
	visited := map[*Node]bool{start: true}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		for _, w := range v.Edges {
			if !in[w] {
				continue
			}
			if w == start {
				path := []*Node{v}
				for p := prev[v]; p != nil; p = prev[p] {
					path = append(path, p)
				}
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				return path
			}
			if !visited[w] {
				visited[w] = true
				prev[w] = v
				queue = append(queue, w)
			}
		}
	}
	return []*Node{start}
}
