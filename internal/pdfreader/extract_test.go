package pdfreader

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestExtractTextAndScoringPassage(t *testing.T) {
	result, err := Extract(testPDF("Scoring: 5 points for each bird"), "rules.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if result.Pages != 1 || !strings.Contains(result.Text, "5 points") || len(result.Excerpts) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRejectsNonPDF(t *testing.T) {
	if _, err := Extract([]byte("not a PDF"), "rules.pdf"); err == nil {
		t.Fatal("expected a non-PDF error")
	}
}

func testPDF(line string) []byte {
	content := "BT /F1 12 Tf 72 720 Td (" + line + ") Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for index, object := range objects {
		offsets = append(offsets, output.Len())
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return output.Bytes()
}
