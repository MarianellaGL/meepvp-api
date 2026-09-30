package services

import (
	"tablescore-api/internal/domain"
	"tablescore-api/internal/store"
	"tablescore-api/ws"
)

type GameRepository struct {
	store.Repository
	hub *ws.Hub
}

func NewGameRepository(repo store.Repository, hub *ws.Hub) *GameRepository {
	return &GameRepository{Repository: repo, hub: hub}
}
func (s *GameRepository) changed(result domain.ScoreSession, err error) (domain.ScoreSession, error) {
	if err == nil {
		s.hub.Broadcast("session:"+result.ID, ws.NewMessage(ws.TypeQueryInvalidate, ws.QueryInvalidatePayload{QueryKey: []string{"sessions", result.ID}}), nil)
	}
	return result, err
}
func (s *GameRepository) CreateSession(tableCode, hostToken, ruleID string, players []domain.Player) (domain.ScoreSession, error) {
	return s.changed(s.Repository.CreateSession(tableCode, hostToken, ruleID, players))
}
func (s *GameRepository) AddPlayer(id, name, userID string) (domain.ScoreSession, error) {
	return s.changed(s.Repository.AddPlayer(id, name, userID))
}
func (s *GameRepository) UpdateScores(id string, values map[string]map[string]int) (domain.ScoreSession, error) {
	return s.changed(s.Repository.UpdateScores(id, values))
}
func (s *GameRepository) SetScore(id, playerID, fieldID string, value int) (domain.ScoreSession, error) {
	return s.changed(s.Repository.SetScore(id, playerID, fieldID, value))
}
func (s *GameRepository) AdjustPoints(id, playerID string, delta int) (domain.ScoreSession, error) {
	return s.changed(s.Repository.AdjustPoints(id, playerID, delta))
}
func (s *GameRepository) SaveBoardPhoto(id string, data []byte, contentType string) (domain.ScoreSession, error) {
	return s.changed(s.Repository.SaveBoardPhoto(id, data, contentType))
}
func (s *GameRepository) PauseSession(id string) (domain.ScoreSession, error) {
	return s.changed(s.Repository.PauseSession(id))
}
func (s *GameRepository) ResumeSession(id string) (domain.ScoreSession, error) {
	return s.changed(s.Repository.ResumeSession(id))
}
func (s *GameRepository) FinishSession(id string) (domain.ScoreSession, error) {
	return s.changed(s.Repository.FinishSession(id))
}
func (s *GameRepository) ReopenSession(id string) (domain.ScoreSession, error) {
	return s.changed(s.Repository.ReopenSession(id))
}
