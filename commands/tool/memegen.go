package tool

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
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

// normalizeToPNG converts any supported image (JPEG, PNG, GIF, WebP stickers)
// into a clean PNG so memegen always accepts the uploaded background.
func normalizeToPNG(raw []byte) ([]byte, error) {
	decoded, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if format == "png" {
		return raw, nil
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, decoded); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// renderMemegen builds a custom meme on memegen using the supplied image as
// the background. The image is parked on litterbox (1h) because memegen needs
// a URL it can fetch.
func renderMemegen(background []byte, top, bottom string) ([]byte, error) {
	pngBg, err := normalizeToPNG(background)
	if err != nil {
		return nil, err
	}

	imgURL, err := utils.UploadToLitterbox(pngBg, "meme.png")
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

// memeSegment escapes a caption according to memegen.link's URL path rules
// (https://api.memegen.link/docs) or returns "_" if blank.
func memeSegment(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "_"
	}
	// Escape in exact order so doubled chars and tilde codes don't collide.
	r := strings.NewReplacer(
		"_", "__",
		"-", "--",
		"~", "~t",
		"?", "~q",
		"&", "~a",
		"%", "~p",
		"#", "~h",
		"/", "~s",
		"\\", "~b",
		"<", "~l",
		">", "~g",
		"\"", "''",
		"\n", "~n",
		" ", "_",
	)
	return url.PathEscape(r.Replace(text))
}
