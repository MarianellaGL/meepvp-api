package pdfreader

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Bound subprocess output even for malformed files; commands never invoke a shell.
type limitedOutput struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedOutput) String() string { return b.buf.String() }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buf.Len() {
		return 0, fmt.Errorf("PDF output exceeds limit")
	}
	return b.buf.Write(p)
}

func pdfCommand(ctx context.Context, limit int, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output := &limitedOutput{limit: limit}
	stderr := &limitedOutput{limit: 4096}
	cmd.Stdout, cmd.Stderr = output, stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("PDF extraction with %s failed: %w", name, err)
	}
	return output.String(), nil
}

func extractWithPoppler(data []byte, fileName string) (Result, error) {
	dir, err := os.MkdirTemp("", "tablescore-pdf-*")
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	input := filepath.Join(dir, "input.pdf")
	if err = os.WriteFile(input, data, 0600); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	info, err := pdfCommand(ctx, 65536, "pdfinfo", input)
	if err != nil {
		return Result{}, err
	}
	pages := 0
	for _, line := range strings.Split(info, "\n") {
		if value, ok := strings.CutPrefix(line, "Pages:"); ok {
			pages, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	if pages < 1 || pages > maxPages {
		return Result{}, fmt.Errorf("PDF must have 1 to %d pages", maxPages)
	}
	plain, err := pdfCommand(ctx, maxTextRunes*4, "pdftotext", "-layout", "-enc", "UTF-8", input, "-")
	if err != nil {
		return Result{}, err
	}
	texts := strings.Split(plain, "\f")
	if len(texts) == pages+1 && strings.TrimSpace(texts[pages]) == "" {
		texts = texts[:pages]
	}
	if len(texts) != pages {
		return Result{}, fmt.Errorf("could not read all PDF pages")
	}
	for i, text := range texts {
		if strings.TrimSpace(text) != "" {
			continue
		}
		// Optional OCR supports scanned and mixed documents on Linux and macOS.
		if _, lookupErr := exec.LookPath("tesseract"); lookupErr != nil {
			if strings.TrimSpace(plain) == "" {
				return Result{}, fmt.Errorf("scanned PDF requires OCR: install tesseract with English and Spanish language data")
			}
			continue
		}
		page := strconv.Itoa(i + 1)
		image := filepath.Join(dir, "page")
		if _, err = pdfCommand(ctx, 4096, "pdftoppm", "-f", page, "-l", page, "-singlefile", "-scale-to", "1600", "-png", input, image); err != nil {
			return Result{}, err
		}
		texts[i], err = pdfCommand(ctx, maxTextRunes*4, "tesseract", image+".png", "stdout", "-l", "eng+spa")
		if err != nil {
			return Result{}, err
		}
		if err = os.Remove(image + ".png"); err != nil {
			return Result{}, err
		}
	}
	// Layout extraction pads columns with spaces; padding is not document text.
	for i, page := range texts {
		lines := strings.Split(page, "\n")
		for j, line := range lines {
			lines[j] = strings.Join(strings.Fields(line), " ")
		}
		texts[i] = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	text := strings.TrimSpace(strings.Join(texts, "\n\n"))
	if len([]rune(text)) > maxTextRunes {
		return Result{}, fmt.Errorf("PDF text is too long")
	}
	if text == "" {
		return Result{}, fmt.Errorf("could not find readable text in this PDF")
	}
	return Result{FileName: fileName, Pages: pages, Text: text, Excerpts: scoringExcerpts(text)}, nil
}
