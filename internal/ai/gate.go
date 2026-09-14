package ai

import (
	"fmt"
	"strings"

	"github.com/hycjack/geogebra-dsl-go/internal/check"
	"github.com/hycjack/geogebra-dsl-go/internal/diag"
)

// GateResult is the outcome of running the ggcm validator on a script.
type GateResult struct {
	OK          bool
	Executable  []string // topologically ordered object ids, empty when !OK
	Diagnostics []string // human/repair-formatted findings, empty when OK
}

// runGate validates a script via the existing ggcm checker and turns the
// receipt into repair-friendly diagnostics. This is the single seam between the
// AI layer and the validator — it depends only on check.Check's *diag.Receipt.
func runGate(script string) *GateResult {
	rc := check.Check([]byte(script), check.Options{})
	res := &GateResult{OK: rc.OK, Executable: append([]string(nil), rc.Executable...)}
	if rc.OK {
		return res
	}
	for _, p := range rc.Errors {
		res.Diagnostics = append(res.Diagnostics, formatProblem(p))
	}
	return res
}

// formatProblem renders one diagnostic plus a repair suggestion from the
// translation table described in DESIGN-AI.md §6.3.
func formatProblem(p diag.Problem) string {
	s := fmt.Sprintf("[%s] %s", p.Code, p.Msg)
	if p.Obj != "" {
		s += "（对象 " + p.Obj + "）"
	}
	if hint := repairHint(p.Code); hint != "" {
		s += "；建议：" + hint
	}
	return s
}

func repairHint(c diag.Code) string {
	switch c {
	case diag.CodeCmdUnknown:
		return "改用命令表内命令或拼对命令名（Point/Line/Circle/…）"
	case diag.CodeCmdArg:
		return "对照该命令的正确签名，修正参数个数/类型"
	case diag.CodeDepUndefined:
		return "先定义被引用对象，或移除该引用"
	case diag.CodeDepCycle:
		return "调整定义顺序，避免对象互相依赖成环"
	case diag.CodeDepRedefine:
		return "保留一个定义，其余改名"
	case diag.CodeGeoDegenerate:
		return "调整坐标或半径，避免重合点/零半径等退化"
	case diag.CodeGoalUnreachable:
		return "确保所有被引用/目标对象都已定义"
	default:
		return ""
	}
}

// normalizeScript strips blank lines and applies common casing fixes so the
// validator (and the teaching output) stays tidy. It is conservative: it only
// removes wholly-empty lines and does not rewrite commands.
func normalizeScript(run []string) string {
	var kept []string
	for _, line := range run {
		if strings.TrimSpace(line) == "" {
			continue
		}
		kept = append(kept, strings.TrimSpace(line))
	}
	return strings.Join(kept, "\n")
}
