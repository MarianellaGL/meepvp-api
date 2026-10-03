package store

import "tablescore-api/internal/domain"

// Repository is the persistence boundary used by the BFF.
type Repository interface {
	SaveRulebooks(books []domain.Rulebook) error
	FindRulebooks(query, language string) ([]domain.Rulebook, error)
	GetRulebook(id string) (domain.Rulebook, error)
	CreateUser(username, passwordHash string) (domain.User, error)
	FindUser(username string) (domain.User, string, error)
	SaveAuthSession(userID, tokenHash string, expiresAt int64) error
	UserByAuthSession(tokenHash string) (domain.User, error)
	DeleteAuthSession(tokenHash string) error
	LinkUserSession(userID, sessionID, playerID string) error
	ListUserSessions(userID string) ([]domain.UserSession, error)
	LinkUserTable(userID, tableCode string) error
	ListUserTables(userID string) ([]domain.Table, error)
	SaveUserAvatar(userID string, data []byte, contentType string) error
	GetUserAvatar(userID string) ([]byte, string, error)
	DeleteUserAvatar(userID string) error
	CreateTable(name string) (domain.Table, error)
	GetTable(code string) (domain.Table, error)
	CreateRule(rule domain.ScoringRule) (domain.ScoringRule, error)
	// ListRules returns public sheets plus the ones owned by userID (if any).
	ListRules(userID string) ([]domain.ScoringRule, error)
	ListUserRules(userID string) ([]domain.ScoringRule, error)
	SearchPublicRules(query string, bggID int) ([]domain.ScoringRule, error)
	GetRule(id string) (domain.ScoringRule, error)
	CreateSession(tableCode, hostToken, ruleID string, players []domain.Player) (domain.ScoreSession, error)
	GetSession(id string) (domain.ScoreSession, error)
	ActiveSessionByTable(code string) (domain.ScoreSession, error)
	AddPlayer(sessionID, name, userID string) (domain.ScoreSession, error)
	UpdateScores(sessionID string, values map[string]map[string]int) (domain.ScoreSession, error)
	SetScore(sessionID, playerID, fieldID string, value int) (domain.ScoreSession, error)
	AdjustPoints(sessionID, playerID string, delta int) (domain.ScoreSession, error)
	PauseSession(id string) (domain.ScoreSession, error)
	ResumeSession(id string) (domain.ScoreSession, error)
	SaveBoardPhoto(id string, data []byte, contentType string) (domain.ScoreSession, error)
	GetBoardPhoto(id string) ([]byte, string, error)
	FinishSession(id string) (domain.ScoreSession, error)
	ReopenSession(id string) (domain.ScoreSession, error)
	CreateScheduledGame(tableCode, hostToken string, game domain.ScheduledGame) (domain.ScheduledGame, error)
	ListScheduledGames(tableCode, hostToken string) ([]domain.ScheduledGame, error)
	UpdateScheduledGame(id, hostToken string, game domain.ScheduledGame) (domain.ScheduledGame, error)
	DeleteScheduledGame(id, hostToken string) error
	SetScheduledGameRule(id, hostToken, ruleID string) (domain.ScheduledGame, error)
	SetScheduledGameSession(id, hostToken, sessionID string) (domain.ScheduledGame, error)
}
