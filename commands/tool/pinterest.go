package tool

import (
	"bot/handlers"
	"bot/session"
	"bot/types"
	"bot/utils"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

var (
	pinVideoMP4Regex  = regexp.MustCompile(`https://v\d*\.pinimg\.com/videos/[^"\\'\s]+\.mp4`)
	pinOrigImageRegex = regexp.MustCompile(`https://i\.pinimg\.com/originals/[a-zA-Z0-9/_-]+\.(?:jpg|jpeg|png|gif|webp)`)
	pinMetaTagRegex   = regexp.MustCompile(`(?i)<meta\s+[^>]*>`)
	pinAttrContentRe  = regexp.MustCompile(`(?i)\bcontent=["']([^"']+)["']`)
	pinAttrPropRe     = regexp.MustCompile(`(?i)\b(?:property|name)=["']([^"']+)["']`)
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "pinterest",
		Description: "Search Pinterest images or download media from a Pinterest link.",
		Category:    "search",
		Execute:     executePinterest,
	})
	handlers.RegisterCommand(types.Command{
		Name:        "pin",
		Description: "Alias for /pinterest.",
		Category:    "search",
		Execute:     executePinterest,
	})
	handlers.RegisterInteractiveSessionHandler(handlePinterestInteraction)
}

type pinterestPinMedia struct {
	VideoURL string
	ImageURL string
	Title    string
}

// fetchPinterestSearch queries Pinterest's web search resource directly using
// a fresh cookie jar and CSRF token (no unreliable third-party wrapper API).
func fetchPinterestSearch(ctx context.Context, query string) ([]string, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{
		Jar:     jar,
		Timeout: 20 * time.Second,
	}

	// Step 1: Load search page once to obtain csrftoken cookie.
	searchPageURL := "https://www.pinterest.com/search/pins/?q=" + url.QueryEscape(query)
	initReq, err := http.NewRequestWithContext(ctx, http.MethodGet, searchPageURL, nil)
	if err != nil {
		return nil, err
	}
	initReq.Header.Set("User-Agent", utils.BrowserUserAgent)
	initReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	initReq.Header.Set("Accept-Language", "en-US,en;q=0.9")

	initResp, err := httpClient.Do(initReq)
	if err != nil {
		return nil, fmt.Errorf("pinterest init request failed: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(initResp.Body, 512*1024))
	initResp.Body.Close()

	pinBase, _ := url.Parse("https://www.pinterest.com/")
	csrfToken := ""
	for _, c := range jar.Cookies(pinBase) {
		if c.Name == "csrftoken" {
			csrfToken = c.Value
			break
		}
	}

	// Step 2: Query BaseSearchResource JSON endpoint.
	sourcePath := "/search/pins/?q=" + url.QueryEscape(query)
	dataPayload, _ := json.Marshal(map[string]any{
		"options": map[string]any{
			"query": query,
			"scope": "pins",
			"rs":    "typed",
		},
		"context": map[string]any{},
	})

	apiURL := "https://www.pinterest.com/resource/BaseSearchResource/get/?" + url.Values{
		"source_url": {sourcePath},
		"data":       {string(dataPayload)},
	}.Encode()

	apiReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	apiReq.Header.Set("User-Agent", utils.BrowserUserAgent)
	apiReq.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	apiReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	apiReq.Header.Set("X-Pinterest-AppState", "active")
	apiReq.Header.Set("X-Pinterest-Source-Url", sourcePath)
	apiReq.Header.Set("X-Pinterest-PWS-Handler", "www/search/[scope].js")
	apiReq.Header.Set("Referer", "https://www.pinterest.com/")
	if csrfToken != "" {
		apiReq.Header.Set("X-CSRFToken", csrfToken)
	}

	apiResp, err := httpClient.Do(apiReq)
	if err != nil {
		return nil, fmt.Errorf("pinterest search request failed: %w", err)
	}
	defer apiResp.Body.Close()

	if apiResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pinterest search returned HTTP %d", apiResp.StatusCode)
	}

	var parsed struct {
		ResourceResponse struct {
			Data struct {
				Results []struct {
					Images struct {
						Orig struct {
							URL string `json:"url"`
						} `json:"orig"`
						Size736 struct {
							URL string `json:"url"`
						} `json:"736x"`
					} `json:"images"`
				} `json:"results"`
			} `json:"data"`
		} `json:"resource_response"`
	}

	if err := json.NewDecoder(apiResp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("failed to parse pinterest search JSON: %w", err)
	}

	seen := make(map[string]bool)
	var urls []string
	for _, r := range parsed.ResourceResponse.Data.Results {
		imgURL := r.Images.Orig.URL
		if imgURL == "" {
			imgURL = r.Images.Size736.URL
		}
		if imgURL != "" && !seen[imgURL] {
			seen[imgURL] = true
			urls = append(urls, imgURL)
		}
	}

	if len(urls) == 0 {
		return nil, fmt.Errorf("no images found")
	}
	return urls, nil
}

// resolvePinterestPin fetches a Pinterest (or pin.it) URL, follows redirects,
// and extracts direct MP4 video or full-resolution image URLs from the HTML.
func resolvePinterestPin(ctx context.Context, rawURL string) (string, *pinterestPinMedia, error) {
	httpClient := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return rawURL, nil, err
	}
	req.Header.Set("User-Agent", utils.BrowserUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := httpClient.Do(req)
	if err != nil {
		return rawURL, nil, err
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL.String()
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return finalURL, nil, err
	}

	media := parsePinterestHTML(string(bodyBytes))
	return finalURL, media, nil
}

// parsePinterestHTML extracts video/image URLs and title from a Pinterest pin page.
func parsePinterestHTML(pageHTML string) *pinterestPinMedia {
	res := &pinterestPinMedia{}

	// 1. Extract meta tags regardless of attribute order (Pinterest puts content before property).
	var ogImage string
	for _, tag := range pinMetaTagRegex.FindAllString(pageHTML, -1) {
		propMatch := pinAttrPropRe.FindStringSubmatch(tag)
		contentMatch := pinAttrContentRe.FindStringSubmatch(tag)
		if len(propMatch) < 2 || len(contentMatch) < 2 {
			continue
		}
		prop := strings.ToLower(propMatch[1])
		val := html.UnescapeString(strings.TrimSpace(contentMatch[1]))
		switch prop {
		case "og:image":
			if ogImage == "" {
				ogImage = val
			}
		case "og:title":
			if res.Title == "" {
				res.Title = val
			}
		}
	}

	// 2. Check for direct MP4 video streams embedded in the pin JSON.
	if mp4s := pinVideoMP4Regex.FindAllString(pageHTML, -1); len(mp4s) > 0 {
		// Prefer 720p/1080p MP4 over thumbnails/low-res variants.
		best := mp4s[0]
		for _, candidate := range mp4s {
			if strings.Contains(candidate, "/720p/") || strings.Contains(candidate, "/1080p/") {
				best = candidate
				break
			}
		}
		res.VideoURL = best
	}

	// 3. Find the matching original image URL if present, otherwise use og:image.
	if ogImage != "" {
		// Extract the image hash/stem from og:image (e.g. /736x/ab/cd/ef/abcdef.jpg)
		// to locate the exact matching /originals/ URL (which may be .png/.gif/.jpg).
		ogBase := filepath.Base(ogImage)
		ogStem := strings.TrimSuffix(ogBase, filepath.Ext(ogBase))
		for _, orig := range pinOrigImageRegex.FindAllString(pageHTML, -1) {
			if ogStem != "" && strings.Contains(orig, ogStem) {
				res.ImageURL = orig
				break
			}
		}
		if res.ImageURL == "" {
			res.ImageURL = ogImage
		}
	} else if orig := pinOrigImageRegex.FindString(pageHTML); orig != "" {
		res.ImageURL = orig
	}

	return res
}

func executePinterest(client *whatsmeow.Client, msg *events.Message, args []string) {
	senderID := msg.Info.Sender.ToNonAD().String()
	query := strings.TrimSpace(strings.Join(args, " "))

	if query == "" {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Please provide a search term or a Pinterest URL.\n*Search:* `/pin cute cats`\n*Download:* `/pin https://pin.it/...`")
		return
	}

	if targetURL := utils.ExtractURL(query); targetURL != "" {
		go downloadPinterestURL(client, msg.Info.Chat, targetURL)
		return
	}

	if !session.AcquireLock(senderID, "pinterest") {
		utils.SendReply(client, msg.Info.Chat, "⏳ You already have an active Pinterest gallery! Please finish it, or wait 30 seconds.")
		return
	}

	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔍 Searching Pinterest for '%s'...", query))

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

		imageURLs, err := fetchPinterestSearch(ctx, query)
		if err != nil {
			fmt.Printf("[PINTEREST] Search error for %q: %v\n", query, err)
			session.ReleaseLock(senderID, "pinterest")
			utils.SendReply(client, msg.Info.Chat, "❌ Sorry, Pinterest search failed or found no results.")
			return
		}

		sess := &session.InteractiveSession{
			Type:   "pinterest",
			Query:  query,
			Page:   0,
			Data:   imageURLs,
			UserID: senderID,
		}

		SendPinterestPage(client, msg.Info.Chat, sess)
	}()
}

func downloadPinterestURL(client *whatsmeow.Client, chatJID waTypes.JID, rawURL string) {
	utils.SendReply(client, chatJID, "📥 Downloading Pinterest media...")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	finalURL, parsed, htmlErr := resolvePinterestPin(ctx, rawURL)
	if htmlErr != nil {
		fmt.Printf("[PINTEREST] HTML resolve warning for %s: %v\n", rawURL, htmlErr)
	}

	caption := "📍 *Pinterest Download*"
	if parsed != nil && parsed.Title != "" {
		caption = fmt.Sprintf("📍 *Pinterest*\n%s", parsed.Title)
	}

	// 1. Try direct MP4 video URL extracted from the pin page.
	if parsed != nil && parsed.VideoURL != "" {
		if err := downloadAndSendPinterestVideoURL(ctx, client, chatJID, parsed.VideoURL, caption); err == nil {
			return
		} else {
			fmt.Printf("[PINTEREST] Direct MP4 send failed (%v), trying yt-dlp...\n", err)
		}
	}

	// 2. If the pin has no direct MP4 (or direct MP4 failed), check if it's a
	//    video pin via yt-dlp when no high-res image was found or when yt-dlp succeeds.
	if parsed == nil || parsed.VideoURL != "" || parsed.ImageURL == "" {
		if err := downloadPinterestViaYtDlp(ctx, client, chatJID, finalURL, caption); err == nil {
			return
		}
	}

	// 3. Direct image / GIF download from Pinterest CDN.
	if parsed != nil && parsed.ImageURL != "" {
		imgData, contentType, err := utils.DownloadHTTPBytes(ctx, parsed.ImageURL, map[string]string{
			"Referer": "https://www.pinterest.com/",
		})
		if err == nil && len(imgData) > 0 {
			if _, sendErr := utils.SendImageData(ctx, client, chatJID, imgData, contentType, caption); sendErr == nil {
				return
			} else {
				fmt.Printf("[PINTEREST] Send image error: %v\n", sendErr)
			}
		} else {
			fmt.Printf("[PINTEREST] Image download error (%s): %v\n", parsed.ImageURL, err)
		}
	}

	// 4. Final fallback: yt-dlp (handles HLS video pins / unusual pin formats).
	if err := downloadPinterestViaYtDlp(ctx, client, chatJID, finalURL, caption); err == nil {
		return
	}

	utils.SendReply(client, chatJID, "❌ Failed to download Pinterest media. Make sure the pin link is public and valid.")
}

func downloadAndSendPinterestVideoURL(ctx context.Context, client *whatsmeow.Client, chatJID waTypes.JID, videoURL, caption string) error {
	data, _, err := utils.DownloadHTTPBytes(ctx, videoURL, map[string]string{
		"Referer": "https://www.pinterest.com/",
	})
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "nopebot-pin-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	rawPath := filepath.Join(workDir, "raw.mp4")
	outPath := filepath.Join(workDir, "out.mp4")
	if err := os.WriteFile(rawPath, data, 0644); err != nil {
		return err
	}
	if err := utils.EnsureWhatsAppVideo(rawPath, outPath); err != nil {
		outPath = rawPath
	}

	finalBytes, err := os.ReadFile(outPath)
	if err != nil {
		return err
	}
	thumb, seconds := utils.ExtractVideoMeta(outPath)
	_, err = utils.SendVideoData(ctx, client, chatJID, finalBytes, caption, seconds, thumb)
	return err
}

func downloadPinterestViaYtDlp(ctx context.Context, client *whatsmeow.Client, chatJID waTypes.JID, pinURL, caption string) error {
	workDir, err := os.MkdirTemp("", "nopebot-pin-yt-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	outPath := filepath.Join(workDir, "pin.mp4")
	if err := utils.DownloadVideo(pinURL, outPath, "1080"); err != nil {
		return err
	}
	videoData, err := os.ReadFile(outPath)
	if err != nil {
		return err
	}
	thumb, seconds := utils.ExtractVideoMeta(outPath)
	_, err = utils.SendVideoData(ctx, client, chatJID, videoData, caption, seconds, thumb)
	return err
}

func SendPinterestPage(client *whatsmeow.Client, chatJID waTypes.JID, sess *session.InteractiveSession) {
	urls := sess.Data.([]string)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Try up to 3 consecutive images in case one CDN URL fails.
	var imageData []byte
	var contentType string
	var err error
	for attempts := 0; attempts < 3 && sess.Page < len(urls); attempts++ {
		imageData, contentType, err = utils.DownloadHTTPBytes(ctx, urls[sess.Page], map[string]string{
			"Referer": "https://www.pinterest.com/",
		})
		if err == nil && len(imageData) > 0 {
			break
		}
		if sess.Page < len(urls)-1 {
			sess.Page++
		} else {
			break
		}
	}

	if err != nil || len(imageData) == 0 {
		session.ReleaseLock(sess.UserID, "pinterest")
		utils.SendReply(client, chatJID, "❌ Failed to fetch Pinterest image.")
		return
	}

	caption := fmt.Sprintf("📍 *Pinterest:* %s\n🖼️ *Image:* %d of %d\n\nReply to this message with:\n*+* : Next Image\n*-* : Previous Image\n*y* : Done\n\n_(Closes automatically in 30s)_", sess.Query, sess.Page+1, len(urls))

	sentMsg, err := utils.SendImageData(ctx, client, chatJID, imageData, contentType, caption)
	if err != nil {
		session.ReleaseLock(sess.UserID, "pinterest")
		utils.SendReply(client, chatJID, "❌ Failed to send Pinterest image.")
		return
	}
	session.StoreInteractiveSession(sentMsg.ID, sess)
}

func handlePinterestInteraction(client *whatsmeow.Client, msg *events.Message) bool {
	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo == nil || ctxInfo.StanzaID == nil {
		return false
	}

	targetMsgID := *ctxInfo.StanzaID
	sess, found := session.GetInteractiveSession(targetMsgID)
	if !found || sess.Type != "pinterest" {
		return false
	}

	if sess.UserID != msg.Info.Sender.ToNonAD().String() {
		utils.SendReply(client, msg.Info.Chat, "⚠️ This is not your search session!")
		return true
	}

	userText := strings.ToLower(strings.TrimSpace(utils.GetTextFromMessage(msg.Message)))
	urls := sess.Data.([]string)

	switch userText {
	case "+":
		session.ResetSessionTimer(targetMsgID)
		if sess.Page < len(urls)-1 {
			sess.Page++
			go SendPinterestPage(client, msg.Info.Chat, sess)
		} else {
			utils.SendReply(client, msg.Info.Chat, "⚠️ No more images found.")
		}
		return true

	case "-":
		session.ResetSessionTimer(targetMsgID)
		if sess.Page > 0 {
			sess.Page--
			go SendPinterestPage(client, msg.Info.Chat, sess)
		} else {
			utils.SendReply(client, msg.Info.Chat, "⚠️ You are on the first image.")
		}
		return true

	case "y":
		utils.SendReply(client, msg.Info.Chat, "✅ Image selected! Closing gallery.")
		session.ClearInteractiveSession(targetMsgID)
		return true
	}

	return false
}
