package catalog

import (
	"sort"
	"strings"
)

// normalize folds a command name for similarity comparison: case-insensitive
// and separators stripped, so GeoGebra's spelling variants and user-script
// conventions all match — TriangleCentre / Triangle_Centre / trianglecenter
// all normalize to TRIANGLECENTER.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r == '_' || r == '-' || r == '.' || r == ' ' || r == '(' || r == ')' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Suggest returns up to n catalog command names closest to want, for
// did-you-mean diagnostics when a command is not in the table. A command whose
// name equals want case-insensitively is never suggested — Lookup would have
// found it. Separator variants (Segment_ / seg-ment) do get suggested, since
// Lookup compares raw uppercase names and would not match them.
func (c *Catalog) Suggest(want string, n int) []string {
	if n <= 0 {
		return nil
	}
	w := normalize(want)
	if w == "" {
		return nil
	}
	type cand struct {
		name string
		d    int
	}
	cs := make([]cand, 0, len(c.byName))
	for key, cmd := range c.byName {
		if strings.EqualFold(key, want) {
			continue // exact case-insensitive match: Lookup would have found it
		}
		// Report the command's display name, not the uppercase map key.
		cs = append(cs, cand{name: cmd.Name, d: levenshtein(w, normalize(key))})
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].d != cs[j].d {
			return cs[i].d < cs[j].d
		}
		return cs[i].name < cs[j].name
	})
	out := make([]string, 0, n)
	for _, cd := range cs {
		if len(out) >= n {
			break
		}
		// Only report plausible typos: within ~30% of the name's length.
		if cd.d > maxInt(2, len(w)/3) {
			break
		}
		out = append(out, cd.name)
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// levenshtein is the classic O(len(a)*len(b)) edit distance. Names are short
// (a few dozen chars) and this runs at most once per unknown command.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := cur[j-1] + 1
			sub := prev[j-1] + cost
			cur[j] = minInt(del, minInt(ins, sub))
		}
		prev = cur
	}
	return prev[len(b)]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
