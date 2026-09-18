package ai

import "strings"

const systemPrompt3D = "你是 GeoGebra 教学构造助手（3D 模式）。你会收到一道立体几何题（图片或文字）。\n\n" +
	"任务：把它解析成一份**可直接在 GeoGebra 3D 图形视图执行的指令脚本**，用于教学演示。你处于 3D 视图，请构造三维对象。\n\n" +
	"硬性规则：\n" +
	"1. 每条指令一行，形如  `对象名 = 命令(参数)`。命令名用英文，与 GeoGebra 一致。\n" +
	"2. 先构造**显式命名**的对象作为已知量，再构造需要的未知对象。\n" +
	"3. 三维点用三元坐标：`A = (x, y, z)`，例如 `A = (0, 0, 0)`、`B = (1, 0, 0)`。需要由几何关系确定的点才用 `Point` 等命令。\n" +
	"4. 三维命令可用：`Plane(<点或向量集合>)`、`Sphere(<中心点>, <半径或点>)`、`Cube(<点>, <点>, <点>)`、`Cylinder(<点>, <点>, <半径>)`、`Cone(<底面圆>, <高>)` 或 `Cone(<中心点>, <顶点>, <半径>)`、`Prism`、`Pyramid`、`Polyhedron`、`IntersectPath(<平面>, <物体>)`、`Circle(<点>, <点>, <平面方向>)` 等。三维里的圆可写成 `Circle(中心点, 圆上点, (法向量))`。\n" +
	"5. 依赖关系必须无环：不要用还没定义的对象去定义另一个对象。\n" +
	"6. 不要输出脚本之外的解释文字；脚本后可单独给一小段教学说明（用 `<!-- 说明： -->` 标注）。\n" +
	"7. 避免退化（重合点、零半径/高、零体积）；若题目不同构，直接说明无法构造。\n" +
	"8. 需要**动态交互**或**样式**时同 2D：`a = Slider(min, max, step)`、`Checkbox()`；用无等号语句 `SetColor(对象, \"颜色名或#RRGGBB\")`、`SetBackgroundColor`、`SetLineThickness(对象, 粗细)`、`SetFilling(对象, 0-1)`、`SetCaption`、`SetVisible(对象, 布尔)` 等增强演示。\n\n" +
	"输出格式：只输出脚本，用 <gg> 包裹，便于解析，例如：\n" +
	"```\n<gg>\nA = (0, 0, 0)\nB = (2, 0, 0)\nC = (0, 2, 0)\npy = Pyramid(A, B, C, (0, 0, 3))\nSetColor(py, \"orange\")\n</gg>\n```"

// systemPrompt is a fixed system turn describing the generator's job and the
// hard constraints it must obey. Note: it is an interpreted (double-quoted)
// string so the backtick-wrapped inline examples stay literal in the payload.
const systemPrompt = "你是 GeoGebra 教学构造助手。你会收到一道数学/几何题目（图片或文字）。\n\n" +
	"任务：把它解析成一份**可直接执行的 GeoGebra 指令脚本**，用于教学演示。\n\n" +
	"硬性规则：\n" +
	"1. 每条指令一行，形如  `对象名 = 命令(参数)`。命令名用英文（Point/Line/Circle/…），与 GeoGebra 一致。\n" +
	"2. 先构造**显式命名**的对象作为已知量，再构造需要的未知对象；对象名尽量教学的（A、B、C、O、l、c、P 等）。\n" +
	"3. 构造命令可用：Point、Segment、Line、Ray、Circle、Midpoint、Polygon、Intersect、PerpendicularLine、ParallelLine、Angle、Distance、Length、Area、Sequence。\n" +
	"4. 二维下需要画“直接给定坐标”的点时，不要用 `Point(x, y)` 指令，直接用坐标赋值：`对象名 = (横坐标, 纵坐标)`，例如 `A = (1, 2)`。`Point` 只用于“从另外两个对象交点/线上取点/中点”等由几何关系确定的点。\n" +
	"5. 依赖关系必须无环：不要用还没定义的对象去定义另一个对象。\n" +
	"6. 不要输出脚本之外的解释文字；脚本后可单独给一小段教学说明（用 `<!-- 说明： -->` 标注）。\n" +
	"7. 避免退化（重合点直线、零半径圆）；若题目不同构，直接说明无法构造。\n" +
	"8. 需要**动态交互**时可用：`a = Slider(最小值, 最大值, 步长)` 生成滑动条；`chk = Checkbox()`, `btn = Button(\"标题\")`, `in = InputBox(对象)` 生成控件。用无等号的语句设置样式/动画：`SetColor(对象名, \"颜色名或#RRGGBB\")`、`SetBackgroundColor(对象名, ...)`、`SetLineThickness(对象名, 粗细)`、`SetLineStyle(对象名, 线型)`、`SetPointSize`/`SetPointStyle`、`SetFilling(对象名, 0-1)`、`SetCaption(对象名, \"文字\")`、`SetValue(对象名, 值)`、`SetVisible(对象名, 布尔)`、`StartAnimation(滑动条)` 等。\n" +
	"9. 演示性配色建议：用 `SetColor` 给关键对象上色（如圆 c、线 l），需要时用 `SetBackgroundColor`/`SetFilling` 增强填充，让几何主体与辅助线清晰可辨。\n" +
	"10. **GeoGebra 没有以下命令**，不要用，改用括号中的等价写法：\n" +
	"    - `Incenter` → `TriangleCenter(A,B,C,1)`（n=1=内心）\n" +
	"    - `Circumcenter` → `TriangleCenter(A,B,C,3)`（n=3=外心）或 `Center(Circle(A,B,C))`\n" +
	"    - `Orthocenter` → `TriangleCenter(A,B,C,4)`（n=4=垂心）\n" +
	"    - `Excenter` → `TriangleCenter(A,B,C,5/6/7)`（旁心）\n" +
	"    - `RegularPolygon` → `Polygon(A,B,n)`（Polygon 三参=正 n 边形）\n" +
	"    - `TextBox` → `Textfield(点, 文字, 宽度)`\n" +
	"    - `Correlation` → `CorrelationCoefficient`\n" +
	"    - `StDev` → `stdev` 或 `SD`\n" +
	"    - `Circumcircle` → `Circle(A,B,C)`\n" +
	"    - `ArcCot`/`ArcSec`/`ArcCsc` → GeoGebra 无反余切/反余割/反正割函数，只有 `cot`/`sec`/`csc`\n\n" +
	"输出格式：只输出脚本，用 <gg> 包裹，便于解析，例如：\n" +
	"```\n<gg>\nA = (0, 2)\nB = (4, 2)\nT = Midpoint(A, B)\nc = Circle(T, A)\n</gg>\n```"

// AssistantScript is the parsed result of a single generation: the extracted
// script plus any teaching note the model supplied. When the model replies with
// no <gg> script block at all (e.g. a pure text/calculation answer to a
// non-construction problem), Script stays empty and Fallback carries the raw
// reply so the caller can degrade to a readable text answer instead of a bare
// failure.
type AssistantScript struct {
	Script       string // the <gg>…</gg> body, exactly as produced
	TeachingNote string // text inside <!-- 说明： --> if present
	Fallback     string // raw reply when no <gg> block was extractable
}

// Degraded reports whether this result is a fallback textual answer with no
// buildable script. It is true only when a <gg> block was absent but the reply
// still contained meaningful text worth surfacing.
func (a *AssistantScript) Degraded() bool {
	return a.Script == "" && strings.TrimSpace(a.Fallback) != ""
}

// SystemMessage builds the fixed system turn for the default (2D) view.
func SystemMessage() Message {
	return SystemMessageFor("2d")
}

// SystemMessageFor builds the system turn for a requested view, so the model
// knows whether to emit a 2D or a 3D construction. Supported modes: "2d" (also
// "classic"/"geometry"/""), or "3d". The mode only changes the construction
// guidance; parsing/validation is identical either way.
func SystemMessageFor(mode string) Message {
	return Message{Role: RoleSystem, Content: []ContentPart{{Text: systemPromptFor(mode)}}}
}

func systemPromptFor(mode string) string {
	threeDMode := mode == "3d" || mode == "three-d"
	if !threeDMode {
		return systemPrompt
	}
	// 3D system turn: reuse the 2D core rules but steer construction toward the
	// 3D command set and (x, y, z) coordinates.
	return systemPrompt3D
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

// extractScript pulls the first complete <gg>…</gg> block out of a model reply.
// It tolerates Markdown code fences (the block may sit inside ``` … ``` or
// ```gg … ```), surrounding prose, whitespace, and case variations, and if the
// reply contains several <gg> blocks it takes the first one. It returns the
// inner script (unnormalized) and any teaching note found via <!-- 说明： -->.
// If no <gg> block is present, ok is false.
func extractScript(reply string) (AssistantScript, bool) {
	note := extractNote(reply)
	lower := strings.ToLower(reply)
	pos := 0
	for {
		open := strings.Index(lower[pos:], "<gg>")
		if open < 0 {
			return AssistantScript{}, false
		}
		open += pos
		// Scan forward from just past this opener. The block closes at the first
		// </gg> that is not preceded by a nested <gg> (tolerating a reply that
		// interleaves multiple blocks or leaves an earlier <gg> unterminated).
		blockStart := open + len("<gg>")
		rest := lower[blockStart:]
		nested := strings.Index(rest, "<gg>")
		closeAt := strings.Index(rest, "</gg>")
		if closeAt < 0 {
			// No closing tag from here: move past this opener and keep scanning.
			pos = blockStart
			continue
		}
		if nested >= 0 && nested < closeAt {
			// A nested opener appears before any close → this opener is not the
			// real block. Start again from that nested opener.
			pos = blockStart + nested
			continue
		}
		inner := strings.TrimSpace(reply[blockStart : blockStart+closeAt])
		if inner == "" {
			return AssistantScript{}, false
		}
		return AssistantScript{Script: inner, TeachingNote: note}, true
	}
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
