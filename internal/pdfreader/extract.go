package pdfreader

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"
)

const MaxFileBytes = 20 << 20
const maxPages = 100
const maxTextRunes = 120000

type Result struct {
	FileName   string             `json:"fileName"`
	Pages      int                `json:"pages"`
	Text       string             `json:"text"`
	Excerpts   []string           `json:"scoringExcerpts"`
	Suggestion *ScoringSuggestion `json:"scoringSuggestion,omitempty"`
}

func Extract(data []byte, fileName string) (result Result, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result, err = Result{}, fmt.Errorf("could not read this PDF")
		}
		if err == nil {
			result.Suggestion = suggestScoring(result.Text)
		}
	}()
	if len(data) < 5 || len(data) > MaxFileBytes || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return Result{}, fmt.Errorf("file must be a PDF up to 20 MB")
	}
	if _, lookupErr := exec.LookPath("pdftotext"); lookupErr == nil {
		return extractWithPoppler(data, fileName)
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Result{}, fmt.Errorf("could not read this PDF: %w", err)
	}
	pages := reader.NumPage()
	if pages < 1 || pages > maxPages {
		return Result{}, fmt.Errorf("PDF must have 1 to %d pages", maxPages)
	}
	var text strings.Builder
	needsOCR := false
	for pageNumber := 1; pageNumber <= pages; pageNumber++ {
		pageText, pageErr := reader.Page(pageNumber).GetPlainText(nil)
		if pageErr != nil {
			if runtime.GOOS == "darwin" {
				return extractWithPDFKit(data, fileName)
			}
			return Result{}, fmt.Errorf("could not read page %d: %w", pageNumber, pageErr)
		}
		if pageNumber > 1 {
			text.WriteString("\n\n")
		}
		if strings.TrimSpace(pageText) == "" {
			needsOCR = true
		}
		text.WriteString(pageText)
		if len([]rune(text.String())) > maxTextRunes {
			return Result{}, fmt.Errorf("PDF text is too long")
		}
	}
	if needsOCR && runtime.GOOS == "darwin" {
		return extractWithPDFKit(data, fileName)
	}
	plainText := strings.TrimSpace(text.String())
	return Result{FileName: fileName, Pages: pages, Text: plainText, Excerpts: scoringExcerpts(plainText)}, nil
}

func scoringExcerpts(text string) []string {
	keywords := []string{"scoring", "score", "points", "victory", "puntuación", "puntuacion", "puntaje", "puntos", "victoria"}
	seen := map[string]bool{}
	result := []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.Join(strings.FieldsFunc(line, unicode.IsSpace), " ")
		if len(line) < 8 || len(line) > 500 {
			continue
		}
		lower := strings.ToLower(line)
		for _, keyword := range keywords {
			if strings.Contains(lower, keyword) && !seen[lower] {
				result = append(result, line)
				seen[lower] = true
				break
			}
		}
		if len(result) == 12 {
			break
		}
	}
	return result
}
