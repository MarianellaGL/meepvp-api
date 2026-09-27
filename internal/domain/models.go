package domain

import "time"

type FieldKind string

const (
	FieldKindCheckbox FieldKind = "checkbox"
	FieldKindCounter  FieldKind = "counter"
	FieldKindManual   FieldKind = "manual"
)

type WinCondition string

const (
	WinConditionHighest WinCondition = "highest_total"
	WinConditionLowest  WinCondition = "lowest_total"
)

type ScoreField struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Kind          FieldKind `json:"kind"`
	PointsPerUnit int       `json:"pointsPerUnit"`
}

type ScoringRule struct {
	ID           string       `json:"id"`
	BGGID        int          `json:"bggId,omitempty"`
	GameName     string       `json:"gameName"`
	Name         string       `json:"name"`
	WinCondition WinCondition `json:"winCondition"`
	Fields       []ScoreField `json:"fields"`
	IsPublic     bool         `json:"isPublic"`
	CreatedAt    time.Time    `json:"createdAt"`
}

type Table struct {
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	HostToken string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
}

type Player struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ScoreSession struct {
	ID           string                    `json:"id"`
	TableCode    string                    `json:"tableCode"`
	RuleID       string                    `json:"ruleId"`
	Players      []Player                  `json:"players"`
	Values       map[string]map[string]int `json:"values"`
	ManualPoints map[string]int            `json:"manualPoints,omitempty"`
	Status       string                    `json:"status"`
	CreatedAt    time.Time                 `json:"createdAt"`
	LastModified time.Time                 `json:"lastModified"`
}

type PDFImport struct {
	ID        string    `json:"id"`
	RuleID    string    `json:"ruleId"`
	Status    string    `json:"status"`
	FileName  string    `json:"fileName"`
	CreatedAt time.Time `json:"createdAt"`
}

type ScheduledGame struct {
	ID          string    `json:"id"`
	TableCode   string    `json:"tableCode"`
	GameName    string    `json:"gameName"`
	RuleID      string    `json:"ruleId,omitempty"`
	ScheduledAt time.Time `json:"scheduledAt"`
	Players     []string  `json:"players"`
	SessionID   string    `json:"sessionId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
}

type UserSession struct {
	Session  ScoreSession
	PlayerID string
}
