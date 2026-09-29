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
	// The base-game cover and the full scoring section distinguish this from
	// Automa, appendices and expansions that may quote the same categories.
	if strings.HasPrefix(normalized, "wingspan a competitive bird-collection, engine-building game for 1-5 players") &&
		hasAll("game end and scoring", "points for each bird card", "points for each bonus card", "points for end-of-round goals", "1 point for each:", "egg on a bird card", "food token cached on a bird card", "card tucked under a bird card", "most unused food tokens wins") {
		return &ScoringSuggestion{GameName: "Wingspan", Fields: []SuggestedField{
			{Name: "Aves: puntos impresos", Kind: domain.FieldKindManual},
			{Name: "Cartas de bonificación", Kind: domain.FieldKindManual},
			{Name: "Objetivos de fin de ronda", Kind: domain.FieldKindManual},
			{Name: "Huevos sobre aves", Kind: domain.FieldKindCounter, PointsPerUnit: 1},
			{Name: "Alimento almacenado sobre aves", Kind: domain.FieldKindCounter, PointsPerUnit: 1},
			{Name: "Cartas debajo de aves", Kind: domain.FieldKindCounter, PointsPerUnit: 1},
		}, Notes: []string{
			"Propuesta para Wingspan base multijugador en inglés. Revisá los campos; no incluye Automa, expansiones ni el modo Dúo.",
			"En aves y bonificaciones ingresá la suma de puntos impresos o conseguidos, no la cantidad de cartas.",
			"Ingresá el total de puntos de los cuatro objetivos de ronda según el lado del tablero elegido. En el lado competitivo, los empates reparten los puestos ocupados y redondean hacia abajo; la app no calcula esa distribución.",
			"Contá solo huevos y alimento sobre las aves, y cartas debajo de ellas. El alimento de tu reserva y las cartas en mano no suman puntos por sí mismos.",
			"La partida termina después de la cuarta ronda. Gana el mayor total; si empatan, gana quien tenga más alimento sin usar. Si persiste el empate, comparten la victoria. La app muestra el empate por puntos y no automatiza ese desempate.",
		}}
	}
	return nil
}
