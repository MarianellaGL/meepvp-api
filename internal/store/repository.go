package store

import "tablescore-api/internal/domain"

// Repository is the persistence boundary used by the BFF.
type Repository interface {
	CreateTable(name string) (domain.Table, error)
	GetTable(code string) (domain.Table, error)
	CreateRule(rule domain.ScoringRule) (domain.ScoringRule, error)
	ListRules() ([]domain.ScoringRule, error)
	GetRule(id string) (domain.ScoringRule, error)
	CreateSession(tableCode, hostToken, ruleID string, players []domain.Player) (domain.ScoreSession, error)
	GetSession(id string) (domain.ScoreSession, error)
	UpdateScores(sessionID string, values map[string]map[string]int) (domain.ScoreSession, error)
	FinishSession(id string) (domain.ScoreSession, error)
	CreatePDFImport(ruleID, fileName string) (domain.PDFImport, error)
	GetPDFImport(id string) (domain.PDFImport, error)
}
