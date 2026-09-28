package pdfreader

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScannedPDFWithPopplerAndOCR(t *testing.T) {
	for _, name := range []string{"pdftotext", "pdfinfo", "pdftoppm", "tesseract"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip("OCR integration requires Poppler and Tesseract")
		}
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(input, testPDF("Scoring: 5 points for each bird"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	imagePath := filepath.Join(dir, "source")
	if _, err := pdfCommand(ctx, 4096, "pdftoppm", "-singlefile", "-scale-to", "1600", "-png", input, imagePath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(imagePath + ".png")
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var raster bytes.Buffer
	if err = jpeg.Encode(&raster, im, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	content := "q 612 0 0 792 0 0 cm /Im1 Do Q"
	scan := encodeTestPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im1 4 0 R >> >> /Contents 5 0 R >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", im.Bounds().Dx(), im.Bounds().Dy(), raster.Len(), raster.String()),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
	result, err := Extract(scan, "scan.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if result.Pages != 1 || !strings.Contains(strings.ToLower(result.Text), "5 points") || len(result.Excerpts) == 0 {
		t.Fatalf("unexpected scanned PDF: %+v", result)
	}
}
