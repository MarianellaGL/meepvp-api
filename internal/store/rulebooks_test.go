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

func TestRulebookMigrationAndPostgresPersistence(t *testing.T) {
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
	defer func() { _ = pool.Close() }()
	schema := "rulebooks_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, base.Exec("CREATE SCHEMA "+schema).Error)
	defer base.Exec("DROP SCHEMA " + schema + " CASCADE")
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := models.OpenDSN(u.String())
	require.NoError(t, err)
	scopedPool, err := db.DB()
	require.NoError(t, err)
	defer func() { _ = scopedPool.Close() }()
	require.NoError(t, models.Migrate(db))
	require.NoError(t, models.Migrate(db))
	repo, err := store.NewPostgresStore(db)
	require.NoError(t, err)
	books, err := repo.FindRulebooks("", "en")
	require.NoError(t, err)
	require.Len(t, books, 2)
	catan, err := repo.GetRulebook("rule-book:en:catan")
	require.NoError(t, err)
	changed := catan
	changed.Name = "CATAN Rulebook"
	changed.Edition = ""
	changed.PDFURL = "https://cdn.1j1ju.com/medias/new.pdf"
	require.NoError(t, repo.SaveRulebooks([]domain.Rulebook{changed}))
	saved, err := repo.GetRulebook(catan.ID)
	require.NoError(t, err)
	require.Equal(t, catan.Edition, saved.Edition)
	require.Equal(t, catan.CreatedAt, saved.CreatedAt)
	require.Equal(t, changed.PDFURL, saved.PDFURL)
	books, err = repo.FindRulebooks("cAtAn", "en")
	require.NoError(t, err)
	require.Len(t, books, 1)
	books, err = repo.FindRulebooks("%", "en")
	require.NoError(t, err)
	require.Empty(t, books)
	books, err = repo.FindRulebooks("", "fr")
	require.NoError(t, err)
	require.Empty(t, books)
	_, err = repo.GetRulebook("missing")
	require.ErrorIs(t, err, store.ErrNotFound)
	rule, err := repo.CreateRule(domain.ScoringRule{RulebookID: catan.ID, GameName: "Catan", Name: "Reviewed", Fields: []domain.ScoreField{{Name: "Settlements", Kind: domain.FieldKindCounter, PointsPerUnit: 1}}})
	require.NoError(t, err)
	restored, err := repo.GetRule(rule.ID)
	require.NoError(t, err)
	require.Equal(t, catan.ID, restored.RulebookID)
	_, err = models.MigrateDown(db)
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Raw("SELECT count(*) FROM scoring_rules").Scan(&count).Error)
	require.Equal(t, int64(1), count)
	require.NoError(t, models.Migrate(db))
}
