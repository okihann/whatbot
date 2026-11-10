package tool

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bot/utils"
)

const (
	memegenCustom = "https://api.memegen.link/images/custom"
	memegenWait   = 25 * time.Second
)

// renderMemegen builds a custom meme on memegen using the supplied image as
// the background. The image is parked on litterbox (1h) because memegen needs
// a URL it can fetch.
func renderMemegen(background []byte, top, bottom string) ([]byte, error) {
	imgURL, err := utils.UploadToLitterbox(background, "meme.png")
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/%s/%s.png?background=%s",
		memegenCustom, memeSegment(top), memeSegment(bottom), url.QueryEscape(imgURL))

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "whatbot/1.0")

	resp, err := (&http.Client{Timeout: memegenWait}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// On failure memegen still returns a 600x600 placeholder alongside a 4xx,
	// so the status code is the only trustworthy success signal.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("memegen: status %d", resp.StatusCode)
	}
	return data, nil
}

// memeSegment turns one caption into a memegen path segment, or "_" if blank.
func memeSegment(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "_"
	}
	// memegen uses underscores for spaces and a bare "/" would break the path.
	text = strings.ReplaceAll(text, "/", "_")
	text = strings.ReplaceAll(text, " ", "_")
	return url.PathEscape(text)
}
