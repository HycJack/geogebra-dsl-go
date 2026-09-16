// Package catalog loads and serves the GeoGebra command database. The full set
// of command-table JSON files (geogebra-commands/*_Commands.json, one per
// category) is embedded via go:embed and merged into a single catalog, so every
// command across all categories is recognized.
package catalog

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed geogebra-commands/*.json
var embedded embed.FS

// TypeExpr is a catalog-typed parameter (e.g. "Point", "Line", "Number",
// "Point", "Vector/Line/Ray", "GeoObject"). The ordering separated by '/'
// expresses allowed alternatives.
type TypeExpr string

// Alternatives splits a TypeExpr into its allowed coarse kinds at '/'.
func (t TypeExpr) Alternatives() []string {
	raw := string(t)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Param is one positional parameter of an overload.
type Param struct {
	Name     string   `json:"name"`
	Role     string   `json:"role"`
	Type     TypeExpr `json:"type"`
	Optional bool     `json:"optional"`
}

// Overload is one valid signature (by param count + types) of a command.
type Overload struct {
	Syntax   string  `json:"syntax"`
	Params   []Param `json:"params"`
	IsVarArg bool    `json:"-"`
}

// Command is a named command with all its overloads.
type Command struct {
	Name      string     `json:"name"`
	URL       string     `json:"url"`
	Overloads []Overload `json:"overloads"`
}

type rawDoc struct {
	Name      string            `json:"name"`
	URL       string            `json:"url"`
	Overloads []json.RawMessage `json:"overloads"`
}

type rawOverload struct {
	Syntax string  `json:"syntax"`
	Params []Param `json:"params"`
}

// Catalog is an in-memory command table indexed by uppercase name.
type Catalog struct {
	byName map[string]*Command
}

// Default loads and merges all embedded command-table JSON files. It is
// exported for tests to build a controlled catalog via Load.
func Default() (*Catalog, error) {
	c := &Catalog{byName: map[string]*Command{}}
	entries, err := fs.Glob(embedded, "geogebra-commands/*.json")
	if err != nil {
		return nil, fmt.Errorf("list embedded catalog: %w", err)
	}
	for _, e := range entries {
		data, err := embedded.ReadFile(e)
		if err != nil {
			return nil, fmt.Errorf("read embedded catalog %s: %w", e, err)
		}
		if err := c.mergeDoc(data); err != nil {
			return nil, fmt.Errorf("parse embedded catalog %s: %w", e, err)
		}
	}
	return c, nil
}

// Load parses a single catalog JSON bytes (with its "commands" map) into a
// fresh Catalog. Used by tests and the merge path.
func Load(data []byte) (*Catalog, error) {
	c := &Catalog{byName: map[string]*Command{}}
	if err := c.mergeDoc(data); err != nil {
		return nil, err
	}
	return c, nil
}

// mergeDoc parses one command-table JSON document and merges its "commands"
// into the catalog. Overloads for a command present in several category files
// accumulate, and same-named commands in different files are unified. The
// field is accepted in both shapes found in the command set: a map keyed by
// command name, or an array of command objects.
func (c *Catalog) mergeDoc(data []byte) error {
	var doc struct {
		Commands json.RawMessage `json:"commands"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse catalog json: %w", err)
	}
	cmds := doc.Commands
	if len(cmds) == 0 {
		return nil
	}
	if cmds[0] == '[' {
		var arr []rawDoc
		if err := json.Unmarshal(cmds, &arr); err != nil {
			return fmt.Errorf("parse catalog commands array: %w", err)
		}
		for _, rd := range arr {
			c.add(rawDoc(rd))
		}
		return nil
	}
	var m map[string]rawDoc
	if err := json.Unmarshal(cmds, &m); err != nil {
		return fmt.Errorf("parse catalog commands map: %w", err)
	}
	for name, rd := range m {
		rd.Name = name
		c.add(rd)
	}
	return nil
}

func (c *Catalog) add(rd rawDoc) {
	key := strings.ToUpper(rd.Name)
	cmd, ok := c.byName[key]
	if !ok {
		cmd = &Command{Name: rd.Name, URL: rd.URL}
		c.byName[key] = cmd
	}
	for _, raw := range rd.Overloads {
		var ro rawOverload
		if err := json.Unmarshal(raw, &ro); err != nil {
			continue // skip malformed overload
		}
		ov := Overload{Syntax: ro.Syntax, Params: ro.Params}
		// Both spellings occur in the catalog: the scripting commands use ASCII
		// "..." while the command pages use the Unicode ellipsis "…". Matching
		// only "..." left 14 overloads (Repeat, Element, Join, Net, Area, If,
		// Zip, TableText, SelectObjects) fixed-arity, so valid variadic calls
		// such as Element(lst, 1, 2, 3) were rejected.
		if hasEllipsis(ro.Syntax) {
			ov.IsVarArg = true
		}
		cmd.Overloads = append(cmd.Overloads, ov)
	}
}

// hasEllipsis reports whether a syntax string marks a variadic overload.
func hasEllipsis(syntax string) bool {
	return strings.Contains(syntax, "...") || strings.Contains(syntax, "\u2026")
}

// Lookup returns the command by the given (case-insensitive) name.
func (c *Catalog) Lookup(name string) (*Command, bool) {
	cmd, ok := c.byName[strings.ToUpper(name)]
	return cmd, ok
}

// Has reports whether the command name exists in the catalog.
func (c *Catalog) Has(name string) bool {
	_, ok := c.Lookup(name)
	return ok
}

// Names returns all command names, sorted, for diagnostics.
func (c *Catalog) Names() []string {
	out := make([]string, 0, len(c.byName))
	for n := range c.byName {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
