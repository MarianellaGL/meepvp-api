package store_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"tablescore-api/internal/domain"
)

func TestPostgresWaitingGameStartsWhenEveryoneJoins(t *testing.T) {
	repo := postgresTestStore(t)
	table, err := repo.CreateTable("Viernes")
	require.NoError(t, err)
	rule, err := repo.CreateRule(domain.ScoringRule{GameName: "Brass: Birmingham", Name: "Base", Fields: []domain.ScoreField{{Name: "Enlaces", Kind: domain.FieldKindManual}}})
	require.NoError(t, err)
	session, err := repo.CreateSession(table.Code, table.HostToken, rule.ID, []domain.Player{{Name: "Mariana"}, {Name: "Lucía"}, {Name: "Tomás"}}, true)
	require.NoError(t, err)
	require.Equal(t, domain.StatusWaiting, session.Status)

	user, err := repo.CreateUser("lucia", "hash")
	require.NoError(t, err)
	session, err = repo.AddPlayer(session.ID, "Lucía", user.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StatusWaiting, session.Status)
	stored, err := repo.GetSession(session.ID)
	require.NoError(t, err)
	require.True(t, stored.Players[1].Joined, "joining is saved")

	session, err = repo.AddPlayer(session.ID, "Tomás", "")
	require.NoError(t, err)
	require.Equal(t, "active", session.Status)
	require.NotNil(t, session.RunningSince)

	other, err := repo.CreateSession(table.Code, table.HostToken, rule.ID, []domain.Player{{Name: "Mariana"}, {Name: "Ana"}}, true)
	require.NoError(t, err)
	other, err = repo.StartSession(other.ID)
	require.NoError(t, err)
	require.Equal(t, "active", other.Status)
	_, err = repo.StartSession(other.ID)
	require.Error(t, err)
}
