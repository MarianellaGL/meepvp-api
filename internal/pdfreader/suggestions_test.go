package pdfreader

import (
	"strings"
	"tablescore-api/internal/domain"
	"testing"
)

func TestReviewedRulebookSuggestions(t *testing.T) {
	everdell := "Everdell\nBase points for cards: 22\nPoint tokens: 14\nProsperity card bonus points: 10\nJourney points: 4\nEvents: 12"
	catan := "Catan\n1 settlement = 1 VP\n1 city = 2 VPs\nLongest Road special card = 2 VPs\nLargest Army special card = 2 VPs\nVictory point (VP) card = 1 VP"
	for _, tc := range []struct{ title, text string }{{"Everdell", everdell}, {"Catan", catan}} {
		t.Run(tc.title, func(t *testing.T) {
			s := suggestScoring(tc.text)
			if s == nil || s.GameName != tc.title || len(s.Fields) != 5 {
				t.Fatalf("unexpected suggestion: %+v", s)
			}
			if tc.title == "Everdell" {
				for _, f := range s.Fields {
					if f.Kind != domain.FieldKindManual || f.PointsPerUnit != 0 {
						t.Fatal("example totals must not become point multipliers")
					}
				}
			} else {
				want := []int{1, 2, 2, 2, 1}
				for i, f := range s.Fields {
					if f.PointsPerUnit != want[i] {
						t.Fatalf("field %d has wrong points", i)
					}
				}
				if s.Fields[2].Kind != domain.FieldKindCheckbox || s.Fields[3].Kind != domain.FieldKindCheckbox {
					t.Fatal("unique bonuses must be checkboxes")
				}
			}
			if len(s.Notes) < 3 {
				t.Fatal("review and game-ending limitations must be shown")
			}
		})
	}
	for _, text := range []string{"Everdell rules", "Catan scores", strings.ReplaceAll(everdell, "Everdell", "Other game"), strings.ReplaceAll(catan, "1 city = 2 VPs", "1 city = 3 VPs")} {
		if suggestScoring(text) != nil {
			t.Fatal("unverified content must not select a template")
		}
	}
}

func TestSubprocessOutputLimit(t *testing.T) {
	b := &limitedOutput{limit: 3}
	if _, err := b.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("d")); err == nil {
		t.Fatal("must reject output above the cap")
	}
	if b.String() != "abc" {
		t.Fatal("must not retain oversized output")
	}
}
