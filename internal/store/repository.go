package store

import "tablescore-api/internal/domain"

// Repository is the persistence boundary used by the BFF.
type Repository interface {
	CreateUser(username, passwordHash string) (domain.User, error)
	FindUser(username string) (domain.User, string, error)
	SaveAuthSession(userID, tokenHash string, expiresAt int64) error
	UserByAuthSession(tokenHash string) (domain.User, error)
	DeleteAuthSession(tokenHash string) error
	LinkUserSession(userID, sessionID, playerID string) error
	ListUserSessions(userID string) ([]domain.UserSession, error)
	CreateTable(name string) (domain.Table, error)
	GetTable(code string) (domain.Table, error)
	CreateRule(rule domain.ScoringRule) (domain.ScoringRule, error)
	ListRules() ([]domain.ScoringRule, error)
	SearchPublicRules(query string, bggID int) ([]domain.ScoringRule, error)
	GetRule(id string) (domain.ScoringRule, error)
	CreateSession(tableCode, hostToken, ruleID string, players []domain.Player) (domain.ScoreSession, error)
	GetSession(id string) (domain.ScoreSession, error)
	AddPlayer(sessionID, name string) (domain.ScoreSession, error)
	UpdateScores(sessionID string, values map[string]map[string]int) (domain.ScoreSession, error)
	SetScore(sessionID, playerID, fieldID string, value int) (domain.ScoreSession, error)
	AdjustPoints(sessionID, playerID string, delta int) (domain.ScoreSession, error)
	FinishSession(id string) (domain.ScoreSession, error)
	ReopenSession(id string) (domain.ScoreSession, error)
	CreatePDFImport(ruleID, fileName string) (domain.PDFImport, error)
	GetPDFImport(id string) (domain.PDFImport, error)
	CreateScheduledGame(tableCode, hostToken string, game domain.ScheduledGame) (domain.ScheduledGame, error)
	ListScheduledGames(tableCode, hostToken string) ([]domain.ScheduledGame, error)
	SetScheduledGameRule(id, hostToken, ruleID string) (domain.ScheduledGame, error)
	SetScheduledGameSession(id, hostToken, sessionID string) (domain.ScheduledGame, error)
}
