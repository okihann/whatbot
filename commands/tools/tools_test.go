package toolscmd

import (
	"bytes"
	"testing"
)

func TestEncodersAndDecodersRoundTrip(t *testing.T) {
	codecs := []string{"b64", "b32", "b85", "hex", "bin", "oct", "rot13", "atbash", "url", "html"}
	input := "Hello Whatbot 123!"
	for _, c := range codecs {
		enc, err := EncodeString(c, input)
		if err != nil {
			t.Fatalf("EncodeString(%s) error: %v", c, err)
		}
		dec, err := DecodeString(c, enc)
		if err != nil {
			t.Fatalf("DecodeString(%s) error: %v", c, err)
		}
		if dec != input {
			t.Fatalf("codec %s roundtrip mismatch: got %q want %q", c, dec, input)
		}
	}

	morse, err := EncodeString("morse", "SOS 123")
	if err != nil || morse == "" {
		t.Fatalf("morse encode failed: %v", err)
	}
	back, err := DecodeString("morse", morse)
	if err != nil || back != "SOS 123" {
		t.Fatalf("morse decode mismatch: got %q err=%v", back, err)
	}
}

func TestCalculatorBarcodeAndPDF(t *testing.T) {
	v, err := EvaluateMath("(12 + 8) * 3 / 4 + 2^3")
	if err != nil || v != 23 {
		t.Fatalf("expected 23, got %v err=%v", v, err)
	}
	if _, err := EvaluateMath("10 / 0"); err == nil {
		t.Fatalf("expected error on division by zero")
	}

	pngBytes := RenderBarcodePNG("WHATBOT-2026")
	if !bytes.HasPrefix(pngBytes, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("expected PNG header from RenderBarcodePNG")
	}

	pdfBytes := WrapJPEGAsPDF([]byte{0xff, 0xd8, 0xff, 0xd9}, 100, 100)
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF-1.4")) {
		t.Fatalf("expected %%PDF-1.4 header from WrapJPEGAsPDF")
	}
}
