package yahoo

import (
	"testing"
	"time"
)

func TestFilterGlobalNewsCandidates(t *testing.T) {
	now := time.Now().UTC()
	articles := []GlobalNewsArticle{
		{Title: "Fed signals surprise rate path as inflation stalls", Topic: "Central Banks", Published: now},
		{Title: "Celebrity couple announces summer wedding plans", Topic: "Culture", Published: now},
		{Title: "OPEC weighs fresh oil output cuts amid supply concerns", Topic: "Energy", Published: now},
	}

	filtered := FilterGlobalNewsCandidates(articles)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered articles, got %d", len(filtered))
	}
	if filtered[0].Title != articles[0].Title || filtered[1].Title != articles[2].Title {
		t.Fatalf("unexpected filtered articles: %#v", filtered)
	}
}

func TestCleanGlobalTitle(t *testing.T) {
	got := cleanGlobalTitle("  OPEC weighs new cuts - Reuters  ")
	want := "OPEC weighs new cuts"
	if got != want {
		t.Fatalf("cleanGlobalTitle() = %q, want %q", got, want)
	}
}
