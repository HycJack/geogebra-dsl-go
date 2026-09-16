// Package deps checks the dependency graph for cycles and produces a
// topological execution order. Fail-closed with the dependency quasi-order.
package deps

import (
	"sort"
	"strings"

	"github.com/hycjack/geogebra-dsl-go/internal/diag"
	"github.com/hycjack/geogebra-dsl-go/internal/ir"
)

// Order topologically sorts the graph by refs. Returns a valid build order and
// any cycle problems (fail-closed: a cycle ⇒ non-empty problems and no order is
// usable, though a best-effort partial order is still returned).
//
// Cycle problems are attributed precisely: each strongly-connected component
// with more than one member (or a self-loop) is reported as one dep/cycle, and
// leftover nodes that merely *depend on* a cycle are reported separately as
// blocked. The old implementation lumped every undischarged node into a single
// "环" list, which pointed the AI repair loop at innocent dependents (D and E
// in A→B↔C→D→E) instead of the actual cycle (B↔C).
func Order(g *ir.Graph) ([]string, []diag.Problem) {
	// dependents[r] lists every object that references r; in[id] is the number
	// of distinct existing refs of id. Built once so the Kahn scan is O(V+E)
	// instead of the O(V²·R) nested scan the old implementation did.
	dependents := map[string][]string{}
	in := map[string]int{}
	for id := range g.Objects {
		in[id] = 0
	}
	// Both loops iterate g.Order (not the Objects map) so the adjacency slices
	// are appended in source order and the whole Kahn output is deterministic.
	for _, id := range g.Order {
		o := g.Objects[id]
		seen := map[string]bool{}
		for _, r := range o.Refs {
			if _, ok := g.Objects[r]; ok && !seen[r] {
				seen[r] = true
				in[id]++
				dependents[r] = append(dependents[r], id)
			}
		}
	}

	// Kahn's algorithm. The initial queue follows g.Order (source order) so the
	// output order is deterministic — the old map-iteration seeding made the
	// executable order nondeterministic across runs.
	queue := []string{}
	for _, id := range g.Order {
		if in[id] == 0 {
			queue = append(queue, id)
		}
	}
	var order []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, dep := range dependents[id] {
			in[dep]--
			if in[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	var probs []diag.Problem
	if len(order) != len(g.Objects) {
		var leftover []string
		for _, id := range g.Order {
			if in[id] > 0 {
				leftover = append(leftover, id)
			}
		}
		// True cycles are the non-trivial strongly-connected components of the
		// leftover-induced subgraph. Everything else leftover is merely blocked
		// behind a cycle and must not be reported as part of it.
		cycles := findSCCs(g, leftover)
		inCycle := map[string]bool{}
		for _, cyc := range cycles {
			for _, m := range cyc {
				inCycle[m] = true
			}
			msg := "依赖存在环："
			if len(cyc) == 1 {
				msg += cyc[0] + " 依赖自身"
			} else {
				sort.Strings(cyc)
				msg += strings.Join(cyc, " ↔ ")
			}
			msg += "，无法确定构建顺序"
			probs = append(probs, diag.Problem{Code: diag.CodeDepCycle, Msg: msg, Obj: cyc[0]})
		}
		var blocked []string
		for _, id := range leftover {
			if !inCycle[id] {
				blocked = append(blocked, id)
			}
		}
		if len(blocked) > 0 {
			probs = append(probs, diag.Problem{
				Code: diag.CodeDepCycle,
				Msg:  "以下对象依赖成环节点，同样无法确定构建顺序：" + join(blocked),
				Obj:  blocked[0],
			})
		}
	}
	return order, probs
}

// findSCCs returns the strongly-connected components of the subgraph induced
// by nodes, restricted to refs that stay inside nodes. A singleton component
// is returned only when it has a self-loop (a node depending on itself), which
// is a degenerate cycle. Tarjan's algorithm, iterative-free recursion (graphs
// here are small; recursion depth equals the longest chain).
func findSCCs(g *ir.Graph, nodes []string) [][]string {
	inNodes := map[string]bool{}
	for _, n := range nodes {
		inNodes[n] = true
	}
	adj := map[string][]string{}
	for _, n := range nodes {
		for _, r := range g.Objects[n].Refs {
			if inNodes[r] {
				adj[n] = append(adj[n], r)
			}
		}
	}
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var sccs [][]string
	next := 0
	var strongconnect func(v string)
	strongconnect = func(v string) {
		index[v] = next
		low[v] = next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range adj[v] {
			if _, seen := index[w]; !seen {
				strongconnect(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] {
				if index[w] < low[v] {
					low[v] = index[w]
				}
			}
		}
		if low[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			if len(comp) > 1 || hasSelfLoop(adj, comp[0]) {
				sccs = append(sccs, comp)
			}
		}
	}
	// Visit in g.Order order (leftover was already collected in that order),
	// so SCC output is deterministic.
	for _, n := range nodes {
		if _, seen := index[n]; !seen {
			strongconnect(n)
		}
	}
	return sccs
}

// hasSelfLoop reports whether v references itself.
func hasSelfLoop(adj map[string][]string, v string) bool {
	for _, w := range adj[v] {
		if w == v {
			return true
		}
	}
	return false
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}
