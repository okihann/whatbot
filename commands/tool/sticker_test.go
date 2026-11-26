package tool

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func makeTestPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 40, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestRenderBratAndMemePNG(t *testing.T) {
	// Short & long brat text both produce valid 512x512 PNGs.
	for _, txt := range []string{"brat", "this is a much longer paragraph that must auto scale down cleanly inside the 512x512 brat sticker canvas without overflowing"} {
		out, err := renderBratPNG(txt)
		if err != nil {
			t.Fatalf("renderBratPNG(%q) failed: %v", txt, err)
		}
		img, err := png.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("invalid PNG from renderBratPNG: %v", err)
		}
		if img.Bounds().Dx() != 512 || img.Bounds().Dy() != 512 {
			t.Fatalf("expected 512x512 brat PNG, got %v", img.Bounds())
		}
	}

	// Local meme rendering produces valid 512x512 PNG.
	memeOut, err := renderMemePNG(makeTestPNG(640, 360), "WHEN THE CODE", "ACTUALLY COMPILES ON THE FIRST TRY")
	if err != nil {
		t.Fatalf("renderMemePNG failed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(memeOut))
	if err != nil {
		t.Fatalf("invalid PNG from renderMemePNG: %v", err)
	}
	if img.Bounds().Dx() != 512 || img.Bounds().Dy() != 512 {
		t.Fatalf("expected 512x512 meme PNG, got %v", img.Bounds())
	}
}

func TestMemeSegmentEscaping(t *testing.T) {
	if got := memeSegment(""); got != "_" {
		t.Errorf("blank segment = %q, want _", got)
	}
	if got := memeSegment("what? 100%"); got != "what~q_100~p" {
		t.Errorf("memeSegment('what? 100%%') = %q, want what~q_100~p", got)
	}
	if got := memeSegment("a/b & c#1"); got != "a~sb_~a_c~h1" {
		t.Errorf("memeSegment('a/b & c#1') = %q, want a~sb_~a_c~h1", got)
	}
}

func TestStickerAndSoundStickerArgParsing(t *testing.T) {
	crop, pack, author := parseStickerArgs([]string{"crop My Pack | Me"})
	if !crop || pack != "My Pack" || author != "Me" {
		t.Errorf("parseStickerArgs got (%v, %q, %q)", crop, pack, author)
	}

	opts := parseSoundStickerArgs([]string{"crop 8s Cool Sounds | Oki"})
	if !opts.Crop || opts.MaxSeconds != 8 || opts.PackName != "Cool Sounds" || opts.Author != "Oki" {
		t.Errorf("parseSoundStickerArgs got %+v", opts)
	}

	cancelOpts := parseSoundStickerArgs([]string{"cancel"})
	if !cancelOpts.Cancel {
		t.Errorf("expected Cancel=true")
	}
}
