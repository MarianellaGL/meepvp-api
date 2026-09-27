package pdfreader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// PDFKit tolerates some valid, selectable-text PDFs whose content streams the
// Go parser cannot read. This fallback is used only on macOS after a parse error.
const pdfKitScript = `
import Foundation
import PDFKit

guard let path = CommandLine.arguments.last,
      let document = PDFDocument(url: URL(fileURLWithPath: path)) else {
    exit(1)
}
let pages = (0..<document.pageCount).map { document.page(at: $0)?.string ?? "" }
let output = try JSONSerialization.data(withJSONObject: ["pages": document.pageCount, "texts": pages])
FileHandle.standardOutput.write(output)
`

func extractWithPDFKit(data []byte, fileName string) (Result, error) {
	file, err := os.CreateTemp("", "tablescore-pdf-*.pdf")
	if err != nil {
		return Result{}, fmt.Errorf("could not prepare PDF fallback: %w", err)
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return Result{}, fmt.Errorf("could not prepare PDF fallback: %w", err)
	}
	if err = file.Close(); err != nil {
		return Result{}, fmt.Errorf("could not prepare PDF fallback: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "swift", "-e", pdfKitScript, file.Name())
	var output, stderr bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("could not read this PDF with PDFKit: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var extracted struct {
		Pages int      `json:"pages"`
		Texts []string `json:"texts"`
	}
	if err := json.Unmarshal(output.Bytes(), &extracted); err != nil {
		return Result{}, fmt.Errorf("could not decode PDFKit text: %w", err)
	}
	if extracted.Pages < 1 || extracted.Pages > maxPages || len(extracted.Texts) != extracted.Pages {
		return Result{}, fmt.Errorf("PDF must have 1 to %d pages", maxPages)
	}
	var text strings.Builder
	for pageNumber, pageText := range extracted.Texts {
		if pageNumber > 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(pageText)
		if len([]rune(text.String())) > maxTextRunes {
			return Result{}, fmt.Errorf("PDF text is too long")
		}
	}
	plainText := strings.TrimSpace(text.String())
	return Result{FileName: fileName, Pages: extracted.Pages, Text: plainText, Excerpts: scoringExcerpts(plainText)}, nil
}
