package store_test

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/store"
	"tablescore-api/models"
)

func TestPostgresScoringRulesAreScopedToOwner(t *testing.T) {
	repo := postgresTestStore(t)
	ana, err := repo.CreateUser("ana", "hash")
	require.NoError(t, err)
	beto, err := repo.CreateUser("beto", "hash")
	require.NoError(t, err)
	fields := []domain.ScoreField{{Name: "Points", Kind: domain.FieldKindCounter, PointsPerUnit: 1}}
	for _, rule := range []domain.ScoringRule{
		{GameName: "Everdell", Name: "Public", IsPublic: true, OwnerID: ana.ID},
		{GameName: "Everdell", Name: "Ana private", OwnerID: ana.ID},
		{GameName: "Everdell", Name: "Anonymous private"},
	} {
		rule.Fields = fields
		_, err := repo.CreateRule(rule)
		require.NoError(t, err)
	}

	names := func(rules []domain.ScoringRule, err error) []string {
		require.NoError(t, err)
		out := []string{}
		for _, rule := range rules {
			require.Empty(t, rule.OwnerID, "owner must not leak through the JSON payload")
			out = append(out, rule.Name)
		}
		return out
	}
	require.ElementsMatch(t, []string{"Public"}, names(repo.ListRules("")))
	require.ElementsMatch(t, []string{"Public"}, names(repo.ListRules(beto.ID)))
	require.ElementsMatch(t, []string{"Public", "Ana private"}, names(repo.ListRules(ana.ID)))
	require.ElementsMatch(t, []string{"Public", "Ana private"}, names(repo.ListUserRules(ana.ID)))
	require.Empty(t, names(repo.ListUserRules(beto.ID)))
}

// postgresTestStore migrates a throwaway schema in TEST_DATABASE_URL.
func postgresTestStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL must point to a dedicated *_test database")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(u.Path, "_test"))
	base, err := models.OpenDSN(dsn)
	require.NoError(t, err)
	pool, err := base.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	schema := "rules_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, base.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { base.Exec("DROP SCHEMA " + schema + " CASCADE") })
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := models.OpenDSN(u.String())
	require.NoError(t, err)
	scopedPool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = scopedPool.Close() })
	require.NoError(t, models.Migrate(db))
	repo, err := store.NewPostgresStore(db)
	require.NoError(t, err)
	return repo
}

func TestPostgresRulebookLinkAndPages(t *testing.T) {
	repo := postgresTestStore(t)
	book := domain.Rulebook{ID: "rule-book:en:everdell", Source: "rule-book.org", SourceID: "everdell", Name: "Everdell Rulebook", Language: "en", PDFURL: "https://cdn.1j1ju.com/medias/a.pdf"}
	require.NoError(t, repo.SaveRulebooks([]domain.Rulebook{book}))
	require.NoError(t, repo.LinkRulebook(book.ID, 199792))
	require.ErrorIs(t, repo.LinkRulebook("missing", 1), store.ErrNotFound)
	// A later catalog refresh must not drop the link.
	book.Name = "Everdell Rulebook (2nd)"
	require.NoError(t, repo.SaveRulebooks([]domain.Rulebook{book}))
	linked, err := repo.GameRulebooks(199792)
	require.NoError(t, err)
	require.Len(t, linked, 1)
	require.Equal(t, 199792, *linked[0].BGGID)

	pages, err := repo.RulebookPages(book.ID)
	require.NoError(t, err)
	require.Empty(t, pages)
	require.NoError(t, repo.SaveRulebookPages(book.ID, []string{"one", "two", "three"}))
	require.NoError(t, repo.SaveRulebookPages(book.ID, []string{"first", "second"}))
	pages, err = repo.RulebookPages(book.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second"}, pages)
}
