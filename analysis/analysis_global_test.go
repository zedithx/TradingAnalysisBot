package analysis

import "testing"

func TestNormalizeImportance(t *testing.T) {
	tests := map[string]string{
		"major":    "major",
		"WATCH":    "watch",
		" ignore ": "ignore",
		"other":    "",
	}

	for input, want := range tests {
		if got := normalizeImportance(input); got != want {
			t.Fatalf("normalizeImportance(%q) = %q, want %q", input, got, want)
		}
	}
}
