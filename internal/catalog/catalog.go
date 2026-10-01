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
	"sync"

	"github.com/hycjack/geogebra-dsl-go/internal/ir"
)

//go:embed geogebra-commands/*.json
var embedded embed.FS

//go:embed cmdmeta.json
var cmdmetaJSON []byte

//go:embed kernel-commands.txt
var kernelCommandsTXT []byte

//go:embed non-kernel-commands.txt
var nonKernelCommandsTXT []byte

// The two vendored command lists above are the authoritative coverage check.
// They replace the old `len(Names()) >= 400` floor, which could only notice the
// table collapsing wholesale: dropping one real command took 575 → 574 and the
// test stayed green. Both directions are now asserted, so a missing command and
// an unexplained extra one are each a hard failure.
//
// kernel-commands.txt was extracted from GeoGebra's own Commands.java:
//
//	grep -oE '^\s{1,2}[A-Z][A-Za-z0-9_]*\([^)]*\)\s*[,;]' Commands.java \
//	  | sed -E 's/^[^A-Za-z]*//; s/\(.*//' | sort -u
//
// Upstream restructured that file into an enum (see kernel-commands.txt's
// header), which is why the older "grep for CommandName constants" recipe
// matched nothing and the 2026-09-18 hallucination sweep deleted nine real
// commands. Re-run the extraction above when bumping against a newer GeoGebra
// and update the count in the file's header.
var (
	kernelCommandNames    = parseNameList(kernelCommandsTXT)
	nonKernelCommandNames = parseNameList(nonKernelCommandsTXT)
)

// parseNameList reads a newline-separated name list, skipping blanks and #
// comments, and upper-cases every name. Entries may be tab-separated
// ("<NAME>\t<category>\t<reason>"); only the first field is the name — taking
// the whole line would make every excused entry unmatchable.
func parseNameList(data []byte) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		name := line
		if i := strings.IndexByte(name, '\t'); i >= 0 {
			name = name[:i]
		}
		if n := strings.ToUpper(strings.TrimSpace(name)); n != "" {
			out[n] = true
		}
	}
	return out
}

// KernelCommandNames returns the vendored authoritative GeoGebra command names.
// Exposed so the coverage test (and any host that wants to cross-check) can
// iterate them.
func KernelCommandNames() []string {
	out := make([]string, 0, len(kernelCommandNames))
	for n := range kernelCommandNames {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

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
	// Alias names the real GeoGebra command when this entry is a plausible
	// name that GeoGebra does NOT have (`Incenter` → `TriangleCenter`).
	// Empty or absent means "this really is a command".
	Alias string `json:"alias"`
}

type rawOverload struct {
	Syntax string  `json:"syntax"`
	Params []Param `json:"params"`
}

// Catalog is an in-memory command table indexed by uppercase name.
type Catalog struct {
	byName map[string]*Command
	// returns maps an uppercase command name to the coarse result kind it
	// produces (e.g. "Point", "Number"). It is the data-driven replacement for
	// a hand-maintained command→kind switch: adding or correcting a command's
	// result type is now a one-line edit in cmdmeta.json, not a code change.
	returns map[string]string
	// scripting is the set of official GeoGebra "Scripting Commands" (the 67
	// that return no object, per the manual). A command is a scripting command
	// regardless of whether some of its members also produce a usable object
	// (Slider, GetTime, Turtle, ReadText, ...).
	scripting map[string]bool
	// aliases maps a name GeoGebra does NOT have to the real command that does
	// (`Incenter` → `TriangleCenter`, `ParallelLine` → `Line`). These entries are
	// deliberately kept OUT of byName: accepting them as commands would make
	// the validator pass scripts GeoGebra then refuses, which breaks the one
	// promise this tool makes. They exist only so cmd/unknown can say which
	// real command to use instead.
	aliases map[string]string
}

// defaultCatalog is built once and reused for the life of the process. Parsing
// ~20 embedded JSON category files on every Check call dominated validation
// cost (≈16ms/call), and the AI repair loop calls Check up to MaxRepair+1 times
// per request — so the catalog is cached behind sync.OnceValues.
var defaultCatalog = sync.OnceValues(func() (*Catalog, error) {
	c := &Catalog{byName: map[string]*Command{}, returns: map[string]string{}, scripting: map[string]bool{}, aliases: map[string]string{}}
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
	if err := c.mergeMeta(cmdmetaJSON); err != nil {
		return nil, fmt.Errorf("parse embedded cmdmeta: %w", err)
	}
	return c, nil
})

// Default loads and merges all embedded command-table JSON files once and
// returns the process-wide cached catalog. It is safe for concurrent use.
func Default() (*Catalog, error) {
	return defaultCatalog()
}

// Load parses a single catalog JSON bytes (with its "commands" map) into a
// fresh Catalog. Used by tests and the merge path. Note: unlike Default, Load
// does NOT merge the cmdmeta returns/scripting data, so KindOf/IsScriptingCommand
// return empty on a Load-built catalog — tests that need kind resolution should
// use Default (or call mergeMeta directly).
func Load(data []byte) (*Catalog, error) {
	c := &Catalog{byName: map[string]*Command{}, returns: map[string]string{}, scripting: map[string]bool{}, aliases: map[string]string{}}
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
	// An entry that names a real replacement is a name GeoGebra does not have.
	// Record the pointer to that real command and stop — see Catalog.aliases.
	if a := strings.TrimSpace(rd.Alias); a != "" {
		c.aliases[key] = a
		return
	}
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

// mergeMeta parses the command metadata document (cmdmeta.json) into the
// catalog: the command→result-kind map and the scripting-command set.
func (c *Catalog) mergeMeta(data []byte) error {
	var doc struct {
		Returns   map[string]string `json:"returns"`
		Scripting []string          `json:"scripting"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse cmdmeta json: %w", err)
	}
	for name, tok := range doc.Returns {
		c.returns[strings.ToUpper(name)] = tok
	}
	for _, name := range doc.Scripting {
		c.scripting[strings.ToUpper(name)] = true
	}
	return nil
}

// KindOf reports the coarse result kind of a command as an ir kind-name token
// ("Point", "Number", ...), or "" when the command's result type is unmodeled.
// A scripting command that produces no usable object resolves to "Script" —
// unless it is one of the few that do (Slider/GetTime/Turtle/ReadText/...),
// whose explicit entry in the returns map wins. Case-insensitive.
func (c *Catalog) KindOf(cmd string) string {
	key := strings.ToUpper(cmd)
	if tok, ok := c.returns[key]; ok {
		return tok
	}
	if c.scripting[key] {
		return "Script"
	}
	return ""
}

// IsScriptingCommand reports whether cmd is an official GeoGebra Scripting
// command — one that returns no object (its result, if any, is "Script"). Case-
// insensitive. This is the single source for the bare-statement legality rule.
func (c *Catalog) IsScriptingCommand(cmd string) bool {
	return c.scripting[strings.ToUpper(cmd)]
}

// ApplyKinds assigns each object's coarse Kind from its command's documented
// result kind when the input left it unknown (text input). IR input carries
// Kind already; literals were typed during build. This is the single place a
// command name becomes an ir.Kind, shared by check.Check and by tests that
// build a graph without running the whole pipeline.
func (c *Catalog) ApplyKinds(g *ir.Graph) {
	for _, id := range g.Order {
		o := g.Objects[id]
		if o.Kind != ir.KUnknown || o.Cmd == "" {
			continue
		}
		if tok := c.KindOf(o.Cmd); tok != "" {
			o.Kind = ir.KindFromToken(tok)
		}
	}
}

// AliasOf reports the real GeoGebra command for a name that looks like a
// command but is not one — the `Incenter` → `TriangleCenter` family, plus
// `ParallelLine` → `Line` and friends. ok is false for names that are genuine
// commands (or that the table has never heard of), so callers must not treat a
// missing entry as "unknown command": that is Lookup's job.
func (c *Catalog) AliasOf(name string) (string, bool) {
	a, ok := c.aliases[strings.ToUpper(name)]
	return a, ok
}

// AliasNames returns every non-command name the table knows about, sorted. Used
// by tests to keep the alias set honest.
func (c *Catalog) AliasNames() []string {
	out := make([]string, 0, len(c.aliases))
	for n := range c.aliases {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
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
