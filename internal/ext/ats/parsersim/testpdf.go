package parsersim

import (
	"bytes"
	"fmt"
)

// MinimalPDF builds a one-page PDF with one /Link annotation carrying the given action dictionary body. It exists so
// golden-good and golden-bad documents are generated deterministically instead of committed as binaries.
func MinimalPDF(action string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> /Annots [6 0 R] >>",
		"", // content stream, filled below
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [20 95 120 115] /Border [0 0 0] /A << %s >> >>", action),
	}
	c := "BT /F1 12 Tf 20 100 Td (Contact me here) Tj ET"
	objs[3] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(c), c)
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	var offs []int
	for i, o := range objs {
		offs = append(offs, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return out.Bytes()
}
