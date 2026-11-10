package utils

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const litterboxEndpoint = "https://litterbox.catbox.moe/resources/internals/api.php"

// UploadToLitterbox stores data for one hour and returns a public URL.
//
// Deliberately not files.catbox.moe: that host resets connections from
// non-browser clients (HTTP/2 PROTOCOL_ERROR), so services like memegen can
// never fetch from it. litter.catbox.moe has no such filter.
func UploadToLitterbox(data []byte, filename string) (string, error) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	if err := w.WriteField("reqtype", "fileupload"); err != nil {
		return "", err
	}
	// 1h is the shortest expiry the API accepts (1, 60 -> 412).
	if err := w.WriteField("time", "1h"); err != nil {
		return "", err
	}
	part, err := w.CreateFormFile("fileToUpload", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", litterboxEndpoint, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Success is a bare URL with no trailing newline; failures are plain text.
	got := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(got, "https://litter.catbox.moe/") {
		return "", fmt.Errorf("litterbox: %s", got)
	}
	return got, nil
}
