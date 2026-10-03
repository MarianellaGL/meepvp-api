package search

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Names as rule-book.org returned them for these queries.
var schottenResults = []string{
	"Ghost Stories: White Moon - Scénario Green Roots Plateau",
	"Schotten Totten Rulebook",
	"Schotten Totten 2 Rulebook",
	"Ghost Stories: White Moon - Scénario Green Roots Rulebook",
	"Harry Potter: Hogwarts Battle Rulebook",
	"Photoshoot Règle",
	"Forgotten Waters Ship's Logs",
	"Tête de Linotte Règle",
	"Trotte Quenotte Règle",
	"Step by Step: Drawing School Rulebook",
}

func TestRankKeepsTheGameAndDropsCatalogNoise(t *testing.T) {
	for _, query := range []string{"schotten totten", "Schotten Totten", "schotten", "shoten toten", "schoten toten", "SCHOTTEN TOTTEN 2"} {
		ranked := Rank(query, schottenResults, func(s string) string { return s })
		require.GreaterOrEqual(t, len(ranked), 1, query)
		require.Contains(t, ranked[0], "Schotten Totten", query)
		for _, name := range ranked {
			require.Contains(t, name, "Schotten Totten", "%q kept noise: %v", query, ranked)
		}
	}
	require.Equal(t, "Schotten Totten 2 Rulebook", Rank("schotten totten 2", schottenResults, func(s string) string { return s })[0])
}

func TestScoreIgnoresAccentsAndPrefersExactNames(t *testing.T) {
	require.Equal(t, 1.0, Score("catán", "Catan Rulebook"))
	require.Greater(t, Score("catan", "Catan"), Score("catan", "Catan: Seafarers"))
	require.Less(t, Score("catan", "Harry Potter: Hogwarts Battle"), MinScore)
}

func TestTitleAndContains(t *testing.T) {
	require.Equal(t, "Schotten Totten", Title("Schotten Totten Rulebook"))
	require.Equal(t, "Tête de Linotte", Title("Tête de Linotte Règle"))
	require.Equal(t, "Everdell", Title("Everdell"))
	require.True(t, Contains("Schotten Totten Rulebook", "schotten"))
	require.False(t, Contains("Schotten Totten Rulebook", "shoten toten"))
}

func TestNormalizeIsSafeAcrossGoroutines(t *testing.T) {
	done := make(chan string, 16)
	for range 16 {
		go func() { done <- Normalize("Catán Rulebook") }()
	}
	for range 16 {
		require.Equal(t, "catan", <-done)
	}
}
