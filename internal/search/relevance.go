// Package search scores catalog names against a user query without any model:
// results are deterministic, free and explainable.
package search

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// MinScore drops fuzzy catalog hits that only share a few letters with the query.
const MinScore = 0.3

var (
	// Words that describe the document rather than the game.
	documentWords = map[string]bool{"rulebook": true, "rulebooks": true, "rules": true, "rule": true, "book": true,
		"regle": true, "regles": true, "reglamento": true, "manual": true}
	documentSuffix = regexp.MustCompile(`(?i)\s*[-–:]?\s*(rule ?book|rules|règles?|reglamento|manual)\s*$`)
	accents        = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
)

// Normalize lowercases, removes accents and punctuation, and drops document words.
func Normalize(s string) string {
	s, _, _ = transform.String(accents, strings.ToLower(s))
	words := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	kept := words[:0]
	for _, word := range words {
		if !documentWords[word] {
			kept = append(kept, word)
		}
	}
	return strings.Join(kept, " ")
}

// Title strips a trailing "Rulebook"/"Règle" so a rulebook name reads as its game.
func Title(name string) string {
	return strings.TrimSpace(documentSuffix.ReplaceAllString(name, ""))
}

// Contains reports whether name already includes the query, ignoring case and accents.
func Contains(name, query string) bool {
	q := Normalize(query)
	return q != "" && strings.Contains(Normalize(name), q)
}

// Score is 1 for an exact name, above 0.5 when the name contains the query
// (shorter names first) and otherwise the trigram similarity, which tolerates typos.
func Score(query, name string) float64 {
	q, n := Normalize(query), Normalize(name)
	if q == "" || n == "" {
		return 0
	}
	if strings.Contains(n, q) {
		return 0.5 + 0.5*float64(len(q))/float64(len(n))
	}
	return similarity(trigrams(q), trigrams(n))
}

// Rank keeps items scoring at least MinScore, best first; ties keep source order.
func Rank[T any](query string, items []T, name func(T) string) []T {
	return order(query, items, name, MinScore)
}

// Sort orders every item by score without dropping any, for sources such as
// BGG that also match alternate names the query cannot see.
func Sort[T any](query string, items []T, name func(T) string) []T {
	return order(query, items, name, -1)
}

func order[T any](query string, items []T, name func(T) string, min float64) []T {
	type scored struct {
		item  T
		score float64
	}
	kept := []scored{}
	for _, item := range items {
		if score := Score(query, name(item)); score >= min {
			kept = append(kept, scored{item, score})
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].score > kept[j].score })
	out := make([]T, len(kept))
	for i, k := range kept {
		out[i] = k.item
	}
	return out
}

// trigrams follows pg_trgm: each word is padded with two leading spaces and one trailing.
func trigrams(s string) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(s) {
		padded := []rune("  " + word + " ")
		for i := 0; i+3 <= len(padded); i++ {
			set[string(padded[i:i+3])] = true
		}
	}
	return set
}

func similarity(a, b map[string]bool) float64 {
	shared := 0
	for gram := range a {
		if b[gram] {
			shared++
		}
	}
	union := len(a) + len(b) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}
