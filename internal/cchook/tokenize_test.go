package cchook

import "testing"

func TestMatchSkipList(t *testing.T) {
	cases := []struct {
		command string
		skip    bool
	}{
		{"ls", true},
		{"ls /tmp", true},
		{"pwd", true},
		{"git status --porcelain=v2", true},
		{"git diff", false},
		{"echo hi", false},
		{"LC_ALL=C LANG=en_US.UTF-8 ls /tmp", true},
		{"LC_ALL=en_US.UTF-8:/tmp/evil ls", false},
		{"PATH=/tmp ls", false},
		{"vim", true},
		{"vim -R", true},
		{"vim file.txt", false},
		{"less file.txt", false},
		{"ls | wc -l", false},
		{"ls; cat secret", false},
		{"'unterminated", false},
		// Documented residual risk: bare $VAR expansions in skip-listed commands
		// pass through unmasked. Captured here so the behavior cannot regress silently.
		{"ls $HOME", true},
		{"ls $HOME/secrets", true},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			got, _ := Match(tc.command)
			if got != tc.skip {
				t.Fatalf("Match(%q) = %v, want %v", tc.command, got, tc.skip)
			}
		})
	}
}

// TestMatchNeverSkipsMaskingBypassVerbs guards a security invariant (R3): the
// verbs echo, test, and [ must never be skip-listed, because skipping them
// would let `echo $SECRET` (or `test ... && echo $secret`) bypass masking.
// It asserts against Match -- the real skip decision -- so a regression that
// adds any of these verbs to Match's switch is caught here.
func TestMatchNeverSkipsMaskingBypassVerbs(t *testing.T) {
	for _, command := range []string{
		"echo $SECRET",
		"test -n x",
		"[ -n x ]",
	} {
		t.Run(command, func(t *testing.T) {
			if skip, _ := Match(command); skip {
				t.Fatalf("Match(%q) = skip; masking-bypass verb must never be skip-listed", command)
			}
		})
	}
}
