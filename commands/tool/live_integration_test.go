package tool

import (
	"bot/utils"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLiveDownloaders runs end-to-end checks against Pinterest, YouTube, and
// TikTok when NOPEBOT_LIVE_TESTS=1 is set in the environment.
func TestLiveDownloaders(t *testing.T) {
	if os.Getenv("NOPEBOT_LIVE_TESTS") != "1" {
		t.Skip("set NOPEBOT_LIVE_TESTS=1 to run live network integration tests")
	}

	t.Run("PinterestSearch", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

		urls, err := fetchPinterestSearch(ctx, "cute cats")
		if err != nil {
			t.Fatalf("fetchPinterestSearch failed: %v", err)
		}
		if len(urls) == 0 {
			t.Fatalf("expected non-empty Pinterest search results")
		}
		t.Logf("Pinterest search returned %d URLs (first: %s)", len(urls), urls[0])
	})

	t.Run("PinterestPinDownload", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

		_, media, err := resolvePinterestPin(ctx, "https://www.pinterest.com/pin/11962755240812416/")
		if err != nil {
			t.Fatalf("resolvePinterestPin failed: %v", err)
		}
		if media == nil || media.ImageURL == "" {
			t.Fatalf("expected image URL from pin, got %+v", media)
		}
		data, ct, err := utils.DownloadHTTPBytes(ctx, media.ImageURL, map[string]string{"Referer": "https://www.pinterest.com/"})
		if err != nil {
			t.Fatalf("failed downloading pin image %s: %v", media.ImageURL, err)
		}
		t.Logf("Pinterest pin image downloaded: %d bytes (%s) from %s", len(data), ct, media.ImageURL)
	})

	t.Run("YouTubeSearchAndInfo", func(t *testing.T) {
		results, err := utils.SearchYouTube("me at the zoo", 5)
		if err != nil {
			t.Fatalf("SearchYouTube failed: %v", err)
		}
		if len(results) == 0 {
			t.Fatalf("expected YouTube search results")
		}
		t.Logf("YouTube search returned %d results (first: %s - %s)", len(results), results[0].Title, results[0].URL)

		info, err := utils.GetVideoInfo("https://www.youtube.com/watch?v=jNQXAC9IVRw", "720")
		if err != nil {
			t.Fatalf("GetVideoInfo failed: %v", err)
		}
		t.Logf("YouTube info: title=%q duration=%.1fs", info.Title, info.Duration)
	})

	t.Run("TikTokTikWMAndTranscode", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		tw, err := fetchTikWM(ctx, "https://www.tiktok.com/@tiktok/video/7106594312292453675")
		if err != nil {
			t.Fatalf("fetchTikWM failed: %v", err)
		}
		playURL := tw.Data.HDPlay
		if playURL == "" {
			playURL = tw.Data.Play
		}
		if playURL == "" {
			t.Fatalf("expected non-empty play URL from TikWM")
		}
		data, _, err := utils.DownloadHTTPBytes(ctx, playURL, nil)
		if err != nil {
			t.Fatalf("downloading TikWM video failed: %v", err)
		}
		dir := t.TempDir()
		raw := filepath.Join(dir, "raw.mp4")
		out := filepath.Join(dir, "out.mp4")
		if err := os.WriteFile(raw, data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := utils.EnsureWhatsAppVideo(raw, out); err != nil {
			t.Fatalf("EnsureWhatsAppVideo failed: %v", err)
		}
		thumb, secs := utils.ExtractVideoMeta(out)
		t.Logf("TikTok video OK: raw=%d bytes, duration=%ds, thumb=%d bytes", len(data), secs, len(thumb))
	})
}
