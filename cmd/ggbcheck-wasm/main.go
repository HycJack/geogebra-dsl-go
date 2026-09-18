//go:build js && wasm

// Command ggbcheck-wasm exposes the ggbcheck validator to a JavaScript host
// (browser or Node) as two synchronous functions.
//
// Build:
//
//	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" \
//	  -o ggbcheck.wasm ./cmd/ggbcheck-wasm
//
// Ship ggbcheck.wasm next to $GOROOT/lib/wasm/wasm_exec.js, then:
//
//	const { instance } = await Go().init(wasmSource)
//	const res = JSON.parse(instance.exports.ggbValidate(script))
//
// res.receipt is the diag.Receipt JSON (ok / errors / warnings / executable),
// so a host can drive a repair loop from it directly. res.script echoes the
// input, so a receipt stays paired with the version it was produced from.
//
// The command catalog (~1 MB of embedded JSON) is parsed once and cached for
// the life of the module; the first call pays for it, or call ggbWarmup to pay
// it eagerly.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/hycjack/geogebra-dsl-go/internal/catalog"
	"github.com/hycjack/geogebra-dsl-go/internal/check"
	"github.com/hycjack/geogebra-dsl-go/internal/diag"
)

// result is the JS-facing envelope of one validation.
type result struct {
	Receipt *diag.Receipt `json:"receipt"`
	Script  string        `json:"script"`
}

func main() {
	global := js.Global()
	global.Set("ggbValidate", js.FuncOf(ggbValidate))
	global.Set("ggbWarmup", js.FuncOf(ggbWarmup))
	select {} // block forever; the module is event-driven from JS
}

// ggbValidate(script, forceSource?) validates a GeoGebra script. forceSource
// may be "text", "ir", or omitted for content sniffing.
func ggbValidate(this js.Value, args []js.Value) interface{} {
	script := ""
	force := ""
	if len(args) > 0 {
		script = args[0].String()
	}
	if len(args) > 1 {
		force = args[1].String()
	}

	res := result{Script: script}
	if script == "" {
		res.Receipt = diag.NewReceipt("text")
		res.Receipt.Fail(diag.Problem{Code: diag.CodeUsage, Msg: "脚本为空"})
		return marshal(res)
	}

	res.Receipt = check.Check([]byte(script), check.Options{ForceSource: force})
	return marshal(res)
}

// ggbWarmup parses the embedded command catalog so the first real validation
// does not pay for it. Returns the number of catalogued commands.
func ggbWarmup(this js.Value, args []js.Value) interface{} {
	type warmupResult struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error,omitempty"`
		Commands int    `json:"commands,omitempty"`
	}
	cat, err := catalog.Default()
	if err != nil {
		b, _ := json.Marshal(warmupResult{Error: err.Error()})
		return string(b)
	}
	b, _ := json.Marshal(warmupResult{OK: true, Commands: len(cat.Names())})
	return string(b)
}

func marshal(res result) string {
	b, err := json.Marshal(res)
	if err != nil {
		return `{"receipt":{"ok":false,"errors":[{"code":"usage","msg":"serialize failed"}],"warnings":[],"executable":[],"source_in":"text"},"script":""}`
	}
	return string(b)
}
