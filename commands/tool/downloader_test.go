package tool

import "testing"

func TestParsePinterestHTML(t *testing.T) {
	sampleImageHTML := `
<html><head>
<meta content="https://i.pinimg.com/736x/89/a1/8a/89a18a3f202dfce9dc1ca2e4535252bc.jpg" data-app="true" name="og:image" property="og:image"/>
<meta content="Cool Art &amp; Stuff" data-app="true" name="og:title" property="og:title"/>
</head><body>
<script>{"url":"https://i.pinimg.com/originals/89/a1/8a/89a18a3f202dfce9dc1ca2e4535252bc.png"}</script>
</body></html>`

	got := parsePinterestHTML(sampleImageHTML)
	if got.ImageURL != "https://i.pinimg.com/originals/89/a1/8a/89a18a3f202dfce9dc1ca2e4535252bc.png" {
		t.Fatalf("expected original png URL, got %q", got.ImageURL)
	}
	if got.Title != "Cool Art & Stuff" {
		t.Fatalf("expected unescaped title, got %q", got.Title)
	}
	if got.VideoURL != "" {
		t.Fatalf("expected empty VideoURL for image pin, got %q", got.VideoURL)
	}

	sampleVideoHTML := `
<html><head>
<meta property="og:image" content="https://i.pinimg.com/736x/11/22/33/112233.jpg"/>
</head><body>
<script>{"v":"https://v1.pinimg.com/videos/mc/expMp4/aa/bb/cc/video_720p.mp4","h":"https://v1.pinimg.com/videos/mc/720p/aa/bb/cc/hd.mp4"}</script>
</body></html>`

	gotVid := parsePinterestHTML(sampleVideoHTML)
	if gotVid.VideoURL != "https://v1.pinimg.com/videos/mc/720p/aa/bb/cc/hd.mp4" {
		t.Fatalf("expected 720p MP4 URL, got %q", gotVid.VideoURL)
	}
	if gotVid.ImageURL != "https://i.pinimg.com/736x/11/22/33/112233.jpg" {
		t.Fatalf("expected og:image fallback, got %q", gotVid.ImageURL)
	}
}

func TestFormatYTDuration(t *testing.T) {
	if got := formatYTDuration(0); got != "LIVE" {
		t.Errorf("formatYTDuration(0) = %q, want LIVE", got)
	}
	if got := formatYTDuration(125); got != "02:05" {
		t.Errorf("formatYTDuration(125) = %q, want 02:05", got)
	}
	if got := formatYTDuration(3661); got != "1:01:01" {
		t.Errorf("formatYTDuration(3661) = %q, want 1:01:01", got)
	}
}

func TestURLClassifiers(t *testing.T) {
	if !isPinterestURL("https://pin.it/xyz123") || !isPinterestURL("https://id.pinterest.com/pin/12345/") {
		t.Errorf("expected pinterest URLs to match")
	}
	if !isTikTokURL("https://vt.tiktok.com/ZSabc/") || !isTikTokURL("https://www.tiktok.com/@user/photo/123") {
		t.Errorf("expected tiktok URLs to match")
	}
	if !isYouTubeLink("https://youtu.be/dQw4w9WgXcQ") || !isYouTubeLink("https://music.youtube.com/watch?v=abc") {
		t.Errorf("expected youtube URLs to match")
	}
}
