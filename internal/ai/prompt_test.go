package ai

import (
	"strings"
	"testing"
)

func TestExtractScript(t *testing.T) {
	reply := "你好\n<gg>\nA = Point(0, 2)\nB = Point(4, 2)\n</gg>\n<!-- 说明：先做点 -->"
	as, ok := extractScript(reply)
	if !ok {
		t.Fatal("expected <gg> block extracted")
	}
	if as.Script != "A = Point(0, 2)\nB = Point(4, 2)" {
		t.Fatalf("script mismatch: %q", as.Script)
	}
	if as.TeachingNote != "先做点" {
		t.Fatalf("note mismatch: %q", as.TeachingNote)
	}
}

func TestExtractScriptNoBlock(t *testing.T) {
	if _, ok := extractScript("no script here"); ok {
		t.Fatal("expected no block")
	}
}

func TestExtractScriptInsideCodeFence(t *testing.T) {
	// Model wraps the whole reply in a Markdown code fence; <gg> sits inside.
	reply := "```gg\n<gg>\nA = (0, 0)\nB = (4, 0)\nc = Circle(A, B)\n</gg>\n```"
	as, ok := extractScript(reply)
	if !ok {
		t.Fatal("expected block extracted from fenced reply")
	}
	if as.Script != "A = (0, 0)\nB = (4, 0)\nc = Circle(A, B)" {
		t.Fatalf("script mismatch: %q", as.Script)
	}
}

func TestExtractScriptTakesFirstBlock(t *testing.T) {
	// Multiple <gg> blocks (e.g. model shows both a draft and the final): the
	// first complete one wins.
	reply := "草稿：\n<gg>\nA = (0, 0)\n</gg>\n最终：\n<gg>\nA = (0, 0)\nB = (4, 0)\nl = Line(A, B)\n</gg>"
	as, ok := extractScript(reply)
	if !ok {
		t.Fatal("expected a block")
	}
	if as.Script != "A = (0, 0)" {
		t.Fatalf("expected first block, got %q", as.Script)
	}
}

func TestExtractScriptUnmatchedOpenSkips(t *testing.T) {
	// An unmatched <gg> without its close should not abort; a later complete
	// block is still found.
	reply := "<gg>\nA = (0, 0)\n<gg>\nB = (4, 0)\nl = Line(A, B)\n</gg>"
	as, ok := extractScript(reply)
	if !ok {
		t.Fatal("expected a complete later block to be found")
	}
	if as.Script != "B = (4, 0)\nl = Line(A, B)" {
		t.Fatalf("script mismatch: %q", as.Script)
	}
}

func TestDegradedTrueOnlyWithText(t *testing.T) {
	if (&AssistantScript{Script: "A = (0, 0)"}).Degraded() {
		t.Fatal("a real script is not degraded")
	}
	if (&AssistantScript{}).Degraded() {
		t.Fatal("empty reply is not degraded")
	}
	if (&AssistantScript{Fallback: "   "}).Degraded() {
		t.Fatal("whitespace-only fallback is not degraded")
	}
	if !(&AssistantScript{Fallback: "答案是 B。(1,3)"}).Degraded() {
		t.Fatal("text-only reply should be degraded")
	}
}

func TestSystemPromptContainsRules(t *testing.T) {
	p := systemPrompt
	for _, want := range []string{"Point", "无环", "退化", "<gg>", "Circle"} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}

func TestSystemMessageFor2DIsDefault(t *testing.T) {
	for _, mode := range []string{"2d", "classic", "geometry", ""} {
		m := SystemMessageFor(mode)
		if len(m.Content) == 0 {
			t.Fatalf("mode %q produced empty message", mode)
		}
		text := m.Content[0].Text
		if strings.Contains(text, "3D 模式") {
			t.Errorf("mode %q should use the 2D prompt, not the 3D one", mode)
		}
		if !strings.Contains(text, "Point") {
			t.Errorf("mode %q missing 2D content", mode)
		}
	}
}

func TestSystemMessageFor3D(t *testing.T) {
	m := SystemMessageFor("3d")
	text := m.Content[0].Text
	for _, want := range []string{"3D 模式", "Sphere", "Pyramid", "Plane", "(x, y, z)", "Cube"} {
		if !strings.Contains(text, want) {
			t.Errorf("3D system prompt missing %q", want)
		}
	}
	sysDefault := SystemMessage()
	if sysDefault.Content[0].Text == text {
		t.Error("3D prompt should differ from the default 2D prompt")
	}
}

func TestImageUserMessageB64(t *testing.T) {
	m := ImageUserMessage("aGVsbG8=", "image/png", "")
	if len(m.Content) != 2 {
		t.Fatalf("expected text+image parts, got %d", len(m.Content))
	}
	if m.Content[1].ImageB64 != "aGVsbG8=" {
		t.Errorf("image b64 not passed through")
	}
}

func TestRepairMessageContent(t *testing.T) {
	m := RepairMessage("l = Line(A, X)", []string{"[dep/undefined] 引用了未定义对象：X"})
	text := m.Content[0].Text
	for _, want := range []string{"完整", "dep/undefined", "上一版脚本"} {
		if !strings.Contains(text, want) {
			t.Errorf("repair message missing %q", want)
		}
	}
}
