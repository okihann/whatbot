package utils

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBuildAndInjectWebPEXIF(t *testing.T) {
	// Minimal valid RIFF/WEBP container with a dummy VP8 chunk.
	dummyVP8 := []byte("VP8 \x04\x00\x00\x00\x01\x02\x03\x04")
	var raw bytes.Buffer
	raw.WriteString("RIFF")
	_ = binary.Write(&raw, binary.LittleEndian, uint32(4+len(dummyVP8)))
	raw.WriteString("WEBP")
	raw.Write(dummyVP8)

	exif := BuildStickerEXIF("MyPack", "MyAuthor", []string{"🔊"})
	if !bytes.Contains(exif, []byte(`"sticker-pack-name":"MyPack"`)) {
		t.Fatalf("EXIF payload missing pack name: %q", string(exif))
	}

	injected := InjectWebPEXIF(raw.Bytes(), exif)
	if len(injected) <= raw.Len() {
		t.Fatalf("expected injected WebP to grow, got %d <= %d", len(injected), raw.Len())
	}
	if string(injected[0:4]) != "RIFF" || string(injected[8:12]) != "WEBP" {
		t.Fatalf("invalid RIFF/WEBP header after injection")
	}
	riffSize := binary.LittleEndian.Uint32(injected[4:8])
	if int(riffSize) != len(injected)-8 {
		t.Fatalf("RIFF size mismatch: header=%d actual=%d", riffSize, len(injected)-8)
	}
	if !bytes.Contains(injected, []byte("VP8X")) {
		t.Fatalf("expected VP8X chunk to be added")
	}
	if !bytes.Contains(injected, []byte("EXIF")) {
		t.Fatalf("expected EXIF chunk to be present")
	}
	if !bytes.Contains(injected, []byte(`"sticker-pack-publisher":"MyAuthor"`)) {
		t.Fatalf("expected author in EXIF chunk")
	}

	// Re-injecting replaces the existing EXIF chunk rather than duplicating it.
	exif2 := BuildStickerEXIF("Pack2", "Author2", nil)
	replaced := InjectWebPEXIF(injected, exif2)
	if bytes.Count(replaced, []byte("EXIF")) != 1 {
		t.Fatalf("expected exactly 1 EXIF chunk after replacement, got %d", bytes.Count(replaced, []byte("EXIF")))
	}
	if !bytes.Contains(replaced, []byte(`"sticker-pack-name":"Pack2"`)) {
		t.Fatalf("expected updated pack name in replaced EXIF")
	}
}

func TestStickerFFmpegPipeline(t *testing.T) {
	ffPath, cleanup, err := GetFFmpegPath()
	if err != nil {
		t.Skipf("ffmpeg unavailable: %v", err)
	}
	defer cleanup()

	if out, err := exec.Command(ffPath, "-version").CombinedOutput(); err != nil || !bytes.Contains(out, []byte("ffmpeg version")) {
		t.Skip("embedded ffmpeg is a placeholder; skipping real encode test")
	}

	// 1. Generate a real 640x360 PNG and convert to static WebP sticker.
	img := image.NewRGBA(image.Rect(0, 0, 640, 360))
	for y := 0; y < 360; y++ {
		for x := 0; x < 640; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 180, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}

	staticWebP, err := ConvertImageToWebP(pngBuf.Bytes(), false, "TestPack", "TestBot")
	if err != nil {
		t.Fatalf("ConvertImageToWebP failed: %v", err)
	}
	if string(staticWebP[0:4]) != "RIFF" || string(staticWebP[8:12]) != "WEBP" {
		t.Fatalf("static WebP missing RIFF/WEBP header")
	}
	if !bytes.Contains(staticWebP, []byte("EXIF")) {
		t.Fatalf("static WebP missing EXIF chunk")
	}
	if len(staticWebP) > MaxStaticStickerBytes {
		t.Fatalf("static WebP exceeds %d bytes: got %d", MaxStaticStickerBytes, len(staticWebP))
	}
	t.Logf("Static WebP OK: %d bytes", len(staticWebP))

	// 2. Generate a synthetic 2-second MP4 with a 440Hz sine wave audio track.
	vidPath := filepath.Join(t.TempDir(), "sample.mp4")
	genCmd := exec.Command(ffPath,
		"-y",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=15",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest",
		vidPath,
	)
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to generate synthetic test video: %v: %s", err, string(out))
	}
	vidBytes, err := os.ReadFile(vidPath)
	if err != nil {
		t.Fatal(err)
	}

	hasAudio, dur := ProbeMediaStreams(vidBytes)
	if !hasAudio || dur < 1.5 {
		t.Fatalf("ProbeMediaStreams expected audio+duration>=1.5s, got hasAudio=%v dur=%.2f", hasAudio, dur)
	}

	animWebP, err := ConvertVideoToAnimatedWebP(vidBytes, 5, true, "SoundPack", "TestBot")
	if err != nil {
		t.Fatalf("ConvertVideoToAnimatedWebP failed: %v", err)
	}
	if string(animWebP[0:4]) != "RIFF" || string(animWebP[8:12]) != "WEBP" || !bytes.Contains(animWebP, []byte("ANIM")) || !bytes.Contains(animWebP, []byte("EXIF")) {
		t.Fatalf("animated WebP missing RIFF/WEBP/ANIM/EXIF chunks")
	}
	t.Logf("Animated WebP OK: %d bytes", len(animWebP))

	oggBytes, secs, err := ConvertToStickerOpus(vidBytes, 5)
	if err != nil {
		t.Fatalf("ConvertToStickerOpus failed: %v", err)
	}
	if len(oggBytes) < 100 || string(oggBytes[0:4]) != "OggS" || secs < 1 {
		t.Fatalf("invalid Opus OGG output: len=%d secs=%d", len(oggBytes), secs)
	}
	t.Logf("Sound Sticker Opus OK: %d bytes, %ds", len(oggBytes), secs)
}
