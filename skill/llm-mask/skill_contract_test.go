package llm_mask_test

import (
	"os"
	"strings"
	"testing"
)

func TestSkillContract(t *testing.T) {
	b, err := os.ReadFile("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	required := []string{
		"name: llm-mask",
		"preserve sentinel tokens verbatim",
		"Never invoke `llm-mask unmask`",
		"tell the user to run `llm-mask unmask < report.md`",
	}
	for _, q := range required {
		if !strings.Contains(s, q) {
			t.Fatalf("missing %q", q)
		}
	}
	if lines := strings.Count(s, "\n") + 1; lines > 500 {
		t.Fatalf("skill too long: %d lines", lines)
	}
}
