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

func TestSystemPromptContainsRules(t *testing.T) {
	p := systemPrompt
	for _, want := range []string{"Point", "无环", "退化", "<gg>", "Circle"} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing %q", want)
		}
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
