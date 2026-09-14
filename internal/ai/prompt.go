package ai

import "strings"

// systemPrompt is a fixed system turn describing the generator's job and the
// hard constraints it must obey. Note: it is an interpreted (double-quoted)
// string so the backtick-wrapped inline examples stay literal in the payload.
const systemPrompt = "你是 GeoGebra 教学构造助手。你会收到一道数学/几何题目（图片或文字）。\n\n" +
	"任务：把它解析成一份**可直接执行的 GeoGebra 指令脚本**，用于教学演示。\n\n" +
	"硬性规则：\n" +
	"1. 每条指令一行，形如  `对象名 = 命令(参数)`。命令名用英文（Point/Line/Circle/…），与 GeoGebra 一致。\n" +
	"2. 先构造**显式命名**的对象作为已知量，再构造需要的未知对象；对象名尽量教学的（A、B、C、O、l、c、P 等）。\n" +
	"3. 只用下面这些基本命令构造，除非题目必要：Point、Segment、Line、Ray、Circle、Midpoint、Polygon、Intersect、PerpendicularLine、ParallelLine、Angle、Distance、Length、Area、Sequence。\n" +
	"4. 二维下需要画“直接给定坐标”的点时，不要用 `Point(x, y)` 指令，直接用坐标赋值：`对象名 = (横坐标, 纵坐标)`，例如 `A = (1, 2)`。`Point` 只用于“从另外两个对象交点/线上取点/中点”等由几何关系确定的点。\n" +
	"5. 依赖关系必须无环：不要用还没定义的对象去定义另一个对象。\n" +
	"6. 不要输出脚本之外的解释文字；脚本后可单独给一小段教学说明（用 `<!-- 说明： -->` 标注）。\n" +
	"7. 避免退化（重合点直线、零半径圆）；若题目不同构，直接说明无法构造。\n\n" +
	"输出格式：只输出脚本，用 <gg> 包裹，便于解析，例如：\n" +
	"```\n<gg>\nA = (0, 2)\nB = (4, 2)\nT = Midpoint(A, B)\nc = Circle(T, A)\n</gg>\n```"

// AssistantScript is the parsed result of a single generation: the extracted
// script plus any teaching note the model supplied.
type AssistantScript struct {
	Script       string // the <gg>…</gg> body, exactly as produced
	TeachingNote string // text inside <!-- 说明： --> if present
}

// SystemMessage builds the fixed system turn.
func SystemMessage() Message {
	return Message{Role: RoleSystem, Content: []ContentPart{{Text: systemPrompt}}}
}

// TextUserMessage wraps a plain-text problem statement as a user turn.
func TextUserMessage(problem string) Message {
	return Message{Role: RoleUser, Content: []ContentPart{{Text: problem}}}
}

// ImageUserMessage wraps an inline image plus a short guide as a user turn.
func ImageUserMessage(imageB64, mime, guide string) Message {
	guide = strings.TrimSpace(guide)
	if guide == "" {
		guide = "请根据这张图片中的几何题目，生成教学用 GeoGebra 指令脚本。"
	}
	return Message{
		Role: RoleUser,
		Content: []ContentPart{
			{Text: guide},
			{ImageB64: imageB64, ImageMIME: mime},
		},
	}
}

// TextAssistantMessage wraps the final script as an assistant turn, stored in
// session history so a later "append"/modify turn can build on the previous
// GeoGebra script rather than regenerate from scratch. When script is empty
// (a failed turn produced no script) it still emits a well-formed, empty block
// so the user→assistant pairing in history stays complete.
func TextAssistantMessage(script string) Message {
	var b strings.Builder
	b.WriteString("<gg>\n")
	if strings.TrimSpace(script) != "" {
		b.WriteString(strings.TrimSpace(script))
		b.WriteString("\n")
	}
	b.WriteString("</gg>")
	return Message{Role: RoleAssistant, Content: []ContentPart{{Text: b.String()}}}
}

// RepairMessage builds the feedback turn for one failed validation round. It
// carries the previous script and every diagnostic so the model knows exactly
// what to fix, and asks for a full replacement rather than a patch.
func RepairMessage(lastScript string, diagnostics []string) Message {
	var b strings.Builder
	b.WriteString("你上一版脚本校验失败，诊断如下（逐条）：\n")
	if len(diagnostics) == 0 {
		b.WriteString("- （无具体错误码，脚本可能结构不完整或无法解析）\n")
	} else {
		for _, d := range diagnostics {
			b.WriteString("- " + d + "\n")
		}
	}
	b.WriteString("请按诊断修正后，重新输出**完整**脚本（不要只输出补丁，不要解释，仍用 <gg> 包裹）。上一版脚本：\n")
	b.WriteString("<gg>\n")
	b.WriteString(lastScript)
	b.WriteString("\n</gg>\n")
	return Message{Role: RoleUser, Content: []ContentPart{{Text: b.String()}}}
}

// extractScript pulls the <gg>…</gg> block out of a model reply. It returns
// the inner script (unnormalized) and any teaching note found via
// <!-- 说明： -->. If no <gg> block is present, ok is false.
func extractScript(reply string) (AssistantScript, bool) {
	lower := strings.ToLower(reply)
	open := strings.Index(lower, "<gg>")
	if open < 0 {
		return AssistantScript{}, false
	}
	rest := reply[open+len("<gg>"):]
	close := strings.Index(lower[open:], "</gg>")
	if close < 0 {
		return AssistantScript{}, false
	}
	inner := rest[:close-len("<gg>")]
	return AssistantScript{Script: strings.TrimSpace(inner), TeachingNote: extractNote(reply)}, true
}

// extractNote finds a teaching note wrapped in <!-- 说明：... -->.
func extractNote(reply string) string {
	const marker = "<!-- 说明："
	i := strings.Index(reply, marker)
	if i < 0 {
		return ""
	}
	rest := reply[i+len(marker):]
	j := strings.Index(rest, "-->")
	if j < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:j])
}
