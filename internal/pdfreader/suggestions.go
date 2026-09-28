package pdfreader

import (
	"strings"
	"tablescore-api/internal/domain"
)

type SuggestedField struct {
	Name          string           `json:"name"`
	Kind          domain.FieldKind `json:"kind"`
	PointsPerUnit int              `json:"pointsPerUnit"`
}

// These are reviewed base-game templates, not arbitrary rules inferred from OCR.
// Require the scoring section as well as the title; filenames alone prove nothing.
type ScoringSuggestion struct {
	GameName string           `json:"gameName"`
	Fields   []SuggestedField `json:"fields"`
	Notes    []string         `json:"notes"`
}

func suggestScoring(text string) *ScoringSuggestion {
	normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
	hasAll := func(anchors ...string) bool {
		for _, anchor := range anchors {
			if !strings.Contains(normalized, anchor) {
				return false
			}
		}
		return true
	}
	if hasAll("everdell", "base points for cards:", "point tokens:", "prosperity card bonus points:", "journey points:", "events:") {
		return &ScoringSuggestion{GameName: "Everdell", Fields: []SuggestedField{
			{Name: "Cartas: puntos base", Kind: domain.FieldKindManual},
			{Name: "Fichas de puntos", Kind: domain.FieldKindManual},
			{Name: "Bonos de prosperidad", Kind: domain.FieldKindManual},
			{Name: "Viajes", Kind: domain.FieldKindManual},
			{Name: "Eventos", Kind: domain.FieldKindManual},
		}, Notes: []string{
			"Propuesta para el juego base multijugador. Revisá los campos antes de guardar; no incluye expansiones ni el modo solitario.",
			"Ingresá los puntos finales de cada categoría, no la cantidad de cartas. Los números del ejemplo del reglamento no son multiplicadores.",
			"Al terminar todos, gana el mayor total. En empate compará cantidad de eventos y luego recursos sobrantes; la app no resuelve esos desempates.",
		}}
	}
	if hasAll("catan", "1 settlement = 1 vp", "1 city = 2 vp", "road special card = 2 vp", "army special card = 2 vp", "card = 1 vp") {
		return &ScoringSuggestion{GameName: "Catan", Fields: []SuggestedField{
			{Name: "Poblados actuales", Kind: domain.FieldKindCounter, PointsPerUnit: 1},
			{Name: "Ciudades", Kind: domain.FieldKindCounter, PointsPerUnit: 2},
			{Name: "Gran ruta comercial", Kind: domain.FieldKindCheckbox, PointsPerUnit: 2},
			{Name: "Gran ejército", Kind: domain.FieldKindCheckbox, PointsPerUnit: 2},
			{Name: "Cartas de puntos de victoria", Kind: domain.FieldKindCounter, PointsPerUnit: 1},
		}, Notes: []string{
			"Propuesta para Catan base. Revisá los campos antes de guardar; no incluye expansiones.",
			"Al convertir un poblado en ciudad, restá un poblado y sumá una ciudad. Los dos poblados iniciales ya cuentan; no agregues dos puntos extra.",
			"Los bonos corresponden al titular actual de cada carta. Las cartas de victoria se mantienen ocultas hasta el final: cargalas al terminar si la planilla es compartida.",
			"Gana quien alcanza 10 puntos durante su propio turno. La app suma puntos, pero no controla turnos ni finaliza automáticamente: confirmá el ganador en la mesa.",
		}}
	}
	return nil
}
