// Command ggbcheck checks whether ai-generated GeoGebra instructions are correct
// and executable. Usage:
//
//	ggbcheck check <file>         validate a text script or IR JSON file
//	ggbcheck check --json <file>  structured receipt on stdout
//
// Exit codes: 0 all-ok / 1 construct not buildable / 2 usage or input error.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/hycjack/geogebra-dsl-go/internal/check"
	"github.com/hycjack/geogebra-dsl-go/internal/diag"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	// Optional leading subcommand "check"; strip it before flag parsing so
	// flags can follow in any order (ggbcheck check --json file / ggbcheck --json check file).
	if len(args) > 0 && args[0] == "check" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("ggbcheck", flag.ContinueOnError)
	useJSON := fs.Bool("json", false, "print structured receipt as JSON")
	force := fs.String("input", "", "force input shape: text | ir (default auto)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "ggbcheck — 校验 AI 生成的 GeoGebra 指令")
		fmt.Fprintln(os.Stderr, "用法: ggbcheck check [--json] [--input text|ir] <文件>")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	path := fs.Arg(0)
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法读取文件:", err)
		return 2
	}
	rc := check.Check(data, check.Options{ForceSource: *force})
	if *useJSON {
		printJSON(rc)
	} else {
		printHuman(rc)
	}
	return exitCode(rc)
}

// exitCode maps a receipt to the documented process exit code:
//   - 0  all checks passed;
//   - 2  usage or input error (usage, parse/syntax, parse/json);
//   - 1  construct not buildable (any other diagnostic).
//
// The exception is that an empty receipt with OK=false (no errors recorded)
// should not happen; OK=false always accompanies at least one error.
func exitCode(rc *diag.Receipt) int {
	if rc.OK {
		return 0
	}
	for _, p := range rc.Errors {
		switch p.Code {
		case diag.CodeUsage, diag.CodeParseSyntax, diag.CodeParseJSON:
			return 2
		}
	}
	return 1
}

func printJSON(rc *diag.Receipt) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(rc)
}

// printHuman renders a Chinese, human-readable report to stdout.
func printHuman(rc *diag.Receipt) {
	if rc.OK {
		fmt.Println("✅ 校验通过：构造可以建立。")
		if len(rc.Executable) > 0 {
			fmt.Printf("   可执行顺序: %v\n", rc.Executable)
		}
		return
	}
	fmt.Println("❌ 校验未通过：")
	for _, p := range rc.Errors {
		loc := ""
		if p.Obj != "" {
			loc = " [" + p.Obj + "]"
		}
		if p.Line > 0 {
			loc = fmt.Sprintf("%s 第%d行", loc, p.Line)
		}
		fmt.Printf("   - [%s] %s%s\n", p.Code, p.Msg, loc)
	}
}
