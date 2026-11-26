package utils

import (
	"bot/config"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	BrowserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

	maxHTTPDownloadBytes = 100 * 1024 * 1024 // 100 MB safety cap
	ytDlpInfoTimeout     = 45 * time.Second
	ytDlpDownloadTimeout = 4 * time.Minute
)

// ErrYouTubeBotCheck indicates YouTube rejected the server IP and requires
// a cookies.txt file to download.
var ErrYouTubeBotCheck = errors.New("youtube requires authentication cookies from this server IP")

type YtDlpResponse struct {
	Title          string          `json:"title"`
	Thumbnail      string          `json:"thumbnail"`
	Filesize       int64           `json:"filesize"`
	FilesizeApprox int64           `json:"filesize_approx"`
	Duration       float64         `json:"duration"`
	Ext            string          `json:"ext"`
	WebpageURL     string          `json:"webpage_url"`
	Entries        []YtDlpResponse `json:"entries"`
}

type YTSearchResult struct {
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	Duration float64 `json:"duration"`
}

var (
	urlPattern = regexp.MustCompile(`https?://[^\s<>"]+`)

	jsRuntimeOnce sync.Once
	jsRuntimeName string
)

// IsValidMediaURL reports whether rawURL is a syntactically valid http(s) URL.
func IsValidMediaURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// ExtractURL returns the first http(s) URL found in text, trimming common
// trailing punctuation from mobile share captions.
func ExtractURL(text string) string {
	match := urlPattern.FindString(text)
	if match == "" {
		return ""
	}
	return strings.TrimRight(match, ".,);]!?\"'")
}

// SanitizeFilename strips characters that are invalid in filenames across OSes.
func SanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
		"\n", " ", "\r", " ",
	)
	clean := strings.TrimSpace(replacer.Replace(name))
	if clean == "" {
		return "media"
	}
	if len(clean) > 120 {
		clean = strings.TrimSpace(clean[:120])
	}
	return clean
}

// detectJSRuntime finds a JavaScript runtime supported by modern yt-dlp (EJS).
func detectJSRuntime() string {
	jsRuntimeOnce.Do(func() {
		for _, candidate := range []string{"deno", "node", "bun", "qjs"} {
			if path, err := exec.LookPath(candidate); err == nil && path != "" {
				if candidate == "qjs" {
					jsRuntimeName = "quickjs:" + path
				} else {
					jsRuntimeName = candidate + ":" + path
				}
				return
			}
		}
	})
	return jsRuntimeName
}

func resolveCookiesFile() string {
	conf, _ := config.LoadConfig()
	if conf != nil && strings.TrimSpace(conf.Downloader.CookiesFile) != "" {
		if _, err := os.Stat(conf.Downloader.CookiesFile); err == nil {
			return conf.Downloader.CookiesFile
		}
		fmt.Println("Warning: Cookies file specified in config.json not found at:", conf.Downloader.CookiesFile)
	}
	if _, err := os.Stat("cookies.txt"); err == nil {
		return "cookies.txt"
	}
	return ""
}

func baseYtDlpArgs() []string {
	args := []string{
		"--no-warnings",
		"--force-ipv4",
		"--no-playlist",
		"--retries", "3",
		"--fragment-retries", "3",
		"--socket-timeout", "15",
	}
	if rt := detectJSRuntime(); rt != "" {
		args = append(args, "--js-runtimes", rt)
	}
	if cookies := resolveCookiesFile(); cookies != "" {
		args = append(args, "--cookies", cookies)
	}
	return args
}

func isYouTubeURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "youtube.com") || strings.Contains(lower, "youtu.be")
}

func classifyYtDlpError(stderr string, err error) error {
	lower := strings.ToLower(stderr)
	if strings.Contains(lower, "sign in to confirm") || strings.Contains(lower, "not a bot") {
		return fmt.Errorf("%w: %s", ErrYouTubeBotCheck, strings.TrimSpace(stderr))
	}
	if strings.TrimSpace(stderr) != "" {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr))
	}
	return err
}

func runYtDlp(ctx context.Context, args []string) ([]byte, string, error) {
	ytDlpPath, cleanup, err := GetYtDlpPath()
	if err != nil {
		return nil, "", err
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, ytDlpPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	return stdout.Bytes(), stderr.String(), err
}

// GetVideoInfo fetches metadata for a media URL using yt-dlp.
func GetVideoInfo(videoURL, quality string) (*YtDlpResponse, error) {
	if !IsValidMediaURL(videoURL) {
		return nil, fmt.Errorf("invalid URL")
	}

	var formatSelector string
	if quality == "" || quality == "best" {
		formatSelector = "bestvideo+bestaudio/best"
	} else {
		formatSelector = fmt.Sprintf("bestvideo[height<=?%s]+bestaudio/best[height<=?%s]/best", quality, quality)
	}

	ctx, cancel := context.WithTimeout(context.Background(), ytDlpInfoTimeout)
	defer cancel()

	args := append(baseYtDlpArgs(),
		"--dump-single-json",
		"-S", "vcodec:h264,res,acodec:m4a",
		"-f", formatSelector,
		"--", videoURL,
	)

	stdout, stderr, err := runYtDlp(ctx, args)
	if err != nil && isYouTubeURL(videoURL) {
		// Retry once with mobile/VR player clients and unconstrained format.
		fallbackArgs := append(baseYtDlpArgs(),
			"--dump-single-json",
			"--extractor-args", "youtube:player_client=android_vr,mweb,web",
			"-f", "bestvideo+bestaudio/best",
			"--", videoURL,
		)
		stdout, stderr, err = runYtDlp(ctx, fallbackArgs)
	}
	if err != nil {
		return nil, fmt.Errorf("yt-dlp info error: %w", classifyYtDlpError(stderr, err))
	}

	var info YtDlpResponse
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &info); err != nil {
		return nil, fmt.Errorf("failed to parse yt-dlp JSON: %w", err)
	}
	if len(info.Entries) > 0 {
		return &info.Entries[0], nil
	}
	return &info, nil
}

// SearchYouTube searches YouTube via yt-dlp flat-playlist search.
func SearchYouTube(query string, limit int) ([]YTSearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(context.Background(), ytDlpInfoTimeout)
	defer cancel()

	args := append(baseYtDlpArgs(),
		"--dump-json",
		"--flat-playlist",
		fmt.Sprintf("ytsearch%d:%s", limit, query),
	)
	stdout, stderr, err := runYtDlp(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("youtube search error: %w", classifyYtDlpError(stderr, err))
	}

	var results []YTSearchResult
	for _, line := range strings.Split(strings.TrimSpace(string(stdout)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item YTSearchResult
		if err := json.Unmarshal([]byte(line), &item); err == nil && item.URL != "" {
			results = append(results, item)
		}
	}
	return results, nil
}

// DownloadVideo downloads videoURL into outputFile as a WhatsApp-compatible
// H.264/AAC MP4.
func DownloadVideo(videoURL, outputFile, quality string) error {
	if !IsValidMediaURL(videoURL) {
		return fmt.Errorf("invalid URL")
	}

	ffmpegPath, ffmpegCleanup, err := GetFFmpegPath()
	if err != nil {
		return fmt.Errorf("could not extract ffmpeg for yt-dlp: %w", err)
	}
	defer ffmpegCleanup()

	workDir, err := os.MkdirTemp("", "whatbot-dl-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	var formatSelector string
	if quality == "" || quality == "best" {
		formatSelector = "bestvideo+bestaudio/best"
	} else {
		formatSelector = fmt.Sprintf("bestvideo[height<=?%s]+bestaudio/best[height<=?%s]/best", quality, quality)
	}

	runDownload := func(extraArgs ...string) (string, string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), ytDlpDownloadTimeout)
		defer cancel()

		outTpl := filepath.Join(workDir, "media.%(ext)s")
		args := append(baseYtDlpArgs(),
			"--ffmpeg-location", ffmpegPath,
			"-S", "vcodec:h264,res,acodec:m4a",
			"-f", formatSelector,
			"--merge-output-format", "mp4",
			"-o", outTpl,
		)
		args = append(args, extraArgs...)
		args = append(args, "--", videoURL)

		_, stderr, dlErr := runYtDlp(ctx, args)
		if dlErr != nil {
			return "", stderr, dlErr
		}
		downloaded, findErr := findDownloadedFile(workDir)
		if findErr != nil {
			return "", stderr, findErr
		}
		return downloaded, stderr, nil
	}

	downloadedFile, stderr, err := runDownload()
	if err != nil && isYouTubeURL(videoURL) {
		// Clean any partial artifacts and retry with fallback player clients.
		cleanDir(workDir)
		downloadedFile, stderr, err = runDownload(
			"--extractor-args", "youtube:player_client=android_vr,mweb,web",
		)
	}
	if err != nil {
		return fmt.Errorf("yt-dlp download error: %w", classifyYtDlpError(stderr, err))
	}

	return EnsureWhatsAppVideo(downloadedFile, outputFile)
}

// DownloadAudioMP3 downloads audio from videoURL and converts it to MP3 at outputFile.
func DownloadAudioMP3(videoURL, outputFile string) error {
	if !IsValidMediaURL(videoURL) {
		return fmt.Errorf("invalid URL")
	}

	ffmpegPath, ffmpegCleanup, err := GetFFmpegPath()
	if err != nil {
		return fmt.Errorf("could not extract ffmpeg for yt-dlp: %w", err)
	}
	defer ffmpegCleanup()

	workDir, err := os.MkdirTemp("", "whatbot-audio-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	runAudio := func(extraArgs ...string) (string, string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), ytDlpDownloadTimeout)
		defer cancel()

		outTpl := filepath.Join(workDir, "audio.%(ext)s")
		args := append(baseYtDlpArgs(),
			"--ffmpeg-location", ffmpegPath,
			"-f", "bestaudio/best",
			"-x", "--audio-format", "mp3",
			"--audio-quality", "0",
			"-o", outTpl,
		)
		args = append(args, extraArgs...)
		args = append(args, "--", videoURL)

		_, stderr, dlErr := runYtDlp(ctx, args)
		if dlErr != nil {
			return "", stderr, dlErr
		}
		downloaded, findErr := findDownloadedFile(workDir)
		if findErr != nil {
			return "", stderr, findErr
		}
		return downloaded, stderr, nil
	}

	downloadedFile, stderr, err := runAudio()
	if err != nil && isYouTubeURL(videoURL) {
		cleanDir(workDir)
		downloadedFile, stderr, err = runAudio(
			"--extractor-args", "youtube:player_client=android_vr,mweb,web",
		)
	}
	if err != nil {
		return fmt.Errorf("yt-dlp audio download error: %w", classifyYtDlpError(stderr, err))
	}

	return copyFile(downloadedFile, outputFile)
}

// EnsureWhatsAppVideo guarantees outputPath is a WhatsApp-playable H.264
// (yuv420p) + AAC MP4 with faststart metadata. When inputPath is already
// compatible, it fast-remuxes without re-encoding.
func EnsureWhatsAppVideo(inputPath, outputPath string) error {
	ffmpegPath, ffCleanup, err := GetFFmpegPath()
	if err != nil {
		return fmt.Errorf("ffmpeg unavailable: %w", err)
	}
	defer ffCleanup()

	if isWhatsAppCompatibleMP4(inputPath) {
		cmd := exec.Command(ffmpegPath,
			"-y",
			"-i", inputPath,
			"-c", "copy",
			"-movflags", "+faststart",
			outputPath,
		)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	cmd := exec.Command(ffmpegPath,
		"-y",
		"-i", inputPath,
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-preset", "veryfast",
		"-crf", "22",
		"-movflags", "+faststart",
		outputPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg transcoding error: %v\nOutput: %s", err, string(out))
	}
	return nil
}

func isWhatsAppCompatibleMP4(videoPath string) bool {
	ffprobePath, cleanup, err := GetFFprobePath()
	if err != nil {
		return false
	}
	defer cleanup()

	out, err := exec.Command(ffprobePath,
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name,pix_fmt",
		"-of", "json",
		videoPath,
	).Output()
	if err != nil {
		return false
	}

	var parsed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			PixFmt    string `json:"pix_fmt"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return false
	}

	hasVideo := false
	for _, s := range parsed.Streams {
		switch s.CodecType {
		case "video":
			hasVideo = true
			if s.CodecName != "h264" || (s.PixFmt != "" && s.PixFmt != "yuv420p") {
				return false
			}
		case "audio":
			if s.CodecName != "aac" && s.CodecName != "mp3" {
				return false
			}
		}
	}
	return hasVideo
}

// DownloadHTTPBytes downloads raw bytes from an HTTP(S) URL with a browser
// User-Agent, timeout, and size cap, returning the data and Content-Type.
func DownloadHTTPBytes(ctx context.Context, rawURL string, extraHeaders map[string]string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", BrowserUserAgent)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	limited := io.LimitReader(resp.Body, maxHTTPDownloadBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxHTTPDownloadBytes {
		return nil, "", fmt.Errorf("file exceeds %d MB limit", maxHTTPDownloadBytes/(1024*1024))
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("empty response body")
	}

	ct := resp.Header.Get("Content-Type")
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	if ct == "" || ct == "application/octet-stream" {
		ct = http.DetectContentType(data)
	}
	return data, ct, nil
}

func findDownloadedFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".ytdl") {
			continue
		}
		return filepath.Join(dir, name), nil
	}
	return "", fmt.Errorf("no downloaded file produced")
}

func cleanDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
