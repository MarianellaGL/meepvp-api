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
	RulebookID   string       `json:"rulebookId,omitempty"`
	ID           string       `json:"id"`
	BGGID        int          `json:"bggId,omitempty"`
	GameName     string       `json:"gameName"`
	Name         string       `json:"name"`
	WinCondition WinCondition `json:"winCondition"`
	Fields       []ScoreField `json:"fields"`
	IsPublic     bool         `json:"isPublic"`
	CreatedAt    time.Time    `json:"createdAt"`
	// OwnerID lives in its own column so the JSON payload never exposes it.
	OwnerID string `json:"-"`
}

// Rulebook stores catalog metadata; PDFs remain at their source URL.
type Rulebook struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	Source    string    `json:"source"`
	SourceID  string    `json:"sourceId"`
	Name      string    `json:"name"`
	Language  string    `json:"language"`
	Edition   string    `json:"edition,omitempty"`
	PDFURL    string    `json:"pdfUrl"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
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
	ID                  string                    `json:"id"`
	TableCode           string                    `json:"tableCode"`
	RuleID              string                    `json:"ruleId"`
	Players             []Player                  `json:"players"`
	Values              map[string]map[string]int `json:"values"`
	ManualPoints        map[string]int            `json:"manualPoints,omitempty"`
	Status              string                    `json:"status"`
	PlayedSeconds       int64                     `json:"playedSeconds"`
	RunningSince        *time.Time                `json:"runningSince,omitempty"`
	PausedAt            *time.Time                `json:"pausedAt,omitempty"`
	BoardPhotoUpdatedAt *time.Time                `json:"boardPhotoUpdatedAt,omitempty"`
	CreatedAt           time.Time                 `json:"createdAt"`
	LastModified        time.Time                 `json:"lastModified"`
	FinishedAt          *time.Time                `json:"finishedAt,omitempty"`
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
