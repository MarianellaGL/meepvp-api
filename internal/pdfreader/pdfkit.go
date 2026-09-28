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

// PDFKit tolerates some PDFs the Go parser cannot read. Vision recognizes text
// on pages without selectable text. This fallback runs only on macOS.
const pdfKitScript = `
import Foundation
import PDFKit
import Vision
import AppKit

guard let path = CommandLine.arguments.last,
      let document = PDFDocument(url: URL(fileURLWithPath: path)) else {
    exit(1)
}
let pages = (0..<document.pageCount).map { index -> String in
    guard let page = document.page(at: index) else { return "" }
    let selectableText = page.string ?? ""
    if !selectableText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
        return selectableText
    }

    let bounds = page.bounds(for: .mediaBox)
    guard bounds.width > 0, bounds.height > 0 else { return "" }
    let width = min(bounds.width * 3, 2400)
    let image = page.thumbnail(of: NSSize(width: width, height: width * bounds.height / bounds.width), for: .mediaBox)
    guard let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
        fputs("could not render PDF page for OCR\n", stderr)
        exit(1)
    }
    let request = VNRecognizeTextRequest()
    request.recognitionLevel = .accurate
    request.usesLanguageCorrection = true
    request.recognitionLanguages = ["es-ES", "en-US"]
    do {
        try VNImageRequestHandler(cgImage: cgImage).perform([request])
    } catch {
        fputs("could not recognize PDF page text: \(error)\n", stderr)
        exit(1)
    }
    return (request.results ?? []).compactMap { $0.topCandidates(1).first?.string }.joined(separator: "\n")
}
let output = try JSONSerialization.data(withJSONObject: ["pages": document.pageCount, "texts": pages])
FileHandle.standardOutput.write(output)
`

func extractWithPDFKit(data []byte, fileName string) (Result, error) {
	file, err := os.CreateTemp("", "tablescore-pdf-*.pdf")
	if err != nil {
		return Result{}, fmt.Errorf("could not prepare PDF fallback: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return Result{}, fmt.Errorf("could not prepare PDF fallback: %w", err)
	}
	if err = file.Close(); err != nil {
		return Result{}, fmt.Errorf("could not prepare PDF fallback: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
