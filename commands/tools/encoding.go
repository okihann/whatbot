package toolscmd

import (
	"bytes"
	"context"
	"encoding/ascii85"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
	"rsc.io/qr"
)

var morseEncodeMap = map[rune]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".", 'F': "..-.",
	'G': "--.", 'H': "....", 'I': "..", 'J': ".---", 'K': "-.-", 'L': ".-..",
	'M': "--", 'N': "-.", 'O': "---", 'P': ".--.", 'Q': "--.-", 'R': ".-.",
	'S': "...", 'T': "-", 'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-",
	'Y': "-.--", 'Z': "--..", '0': "-----", '1': ".----", '2': "..---",
	'3': "...--", '4': "....-", '5': ".....", '6': "-....", '7': "--...",
	'8': "---..", '9': "----.", ' ': "/",
}

var morseDecodeMap = func() map[string]rune {
	m := map[string]rune{}
	for k, v := range morseEncodeMap {
		m[v] = k
	}
	return m
}()

func init() {
	cmds := []types.Command{
		{Name: "encode", Category: "tools", Description: "Encode text. Usage: .encode <b64|b32|b85|hex|bin|oct|rot13|atbash|morse|url|html> <text>", Execute: cmdEncode},
		{Name: "decode", Category: "tools", Description: "Decode text. Usage: .decode <b64|b32|b85|hex|bin|oct|rot13|atbash|morse|url|html|jwt> <text>", Execute: cmdDecode},
		{Name: "b64", Aliases: []string{"base64"}, Category: "tools", Description: "Base64 encode/decode. Usage: .b64 [enc|dec] <text>", Execute: cmdCodecShortcut("b64")},
		{Name: "b32", Aliases: []string{"base32"}, Category: "tools", Description: "Base32 encode/decode. Usage: .b32 [enc|dec] <text>", Execute: cmdCodecShortcut("b32")},
		{Name: "ascii85", Aliases: []string{"base85", "b85"}, Category: "tools", Description: "ASCII85/Base85 encode/decode.", Execute: cmdCodecShortcut("b85")},
		{Name: "hex", Category: "tools", Description: "Hexadecimal encode/decode.", Execute: cmdCodecShortcut("hex")},
		{Name: "bin", Aliases: []string{"binary"}, Category: "tools", Description: "Binary encode/decode.", Execute: cmdCodecShortcut("bin")},
		{Name: "oct", Aliases: []string{"octal"}, Category: "tools", Description: "Octal encode/decode.", Execute: cmdCodecShortcut("oct")},
		{Name: "rot13", Category: "tools", Description: "Apply ROT13 cipher.", Execute: cmdCodecShortcut("rot13")},
		{Name: "atbash", Category: "tools", Description: "Apply Atbash cipher.", Execute: cmdCodecShortcut("atbash")},
		{Name: "morse", Category: "tools", Description: "Encode/decode Morse code.", Execute: cmdCodecShortcut("morse")},
		{Name: "url", Category: "tools", Description: "URL percent-encode/decode.", Execute: cmdCodecShortcut("url")},
		{Name: "html", Category: "tools", Description: "HTML entity escape/unescape.", Execute: cmdCodecShortcut("html")},
		{Name: "jwt", Category: "tools", Description: "Decode a JWT header & payload (without verifying secret).", Execute: cmdJWT},

		// Text, math, visual utility tools
		{Name: "upper", Category: "tools", Description: "Convert text to UPPERCASE.", Execute: cmdTextTransform("upper")},
		{Name: "lower", Category: "tools", Description: "Convert text to lowercase.", Execute: cmdTextTransform("lower")},
		{Name: "reverse", Category: "tools", Description: "Reverse text characters.", Execute: cmdTextTransform("reverse")},
		{Name: "lorem", Category: "tools", Description: "Generate Lorem Ipsum placeholder text. Usage: .lorem [paragraphs]", Execute: cmdLorem},
		{Name: "readmore", Category: "tools", Description: "Create a WhatsApp Read More spoiler message. Usage: .readmore front|hidden", Execute: cmdReadMore},
		{Name: "calculator", Aliases: []string{"calc"}, Category: "tools", Description: "Evaluate a math expression. Usage: .calc (12+8)*3/4", Execute: cmdCalculator},
		{Name: "color", Category: "tools", Description: "Preview a hex color swatch and RGB/HSL values. Usage: .color #00a884", Execute: cmdColor},
		{Name: "qr", Category: "tools", Description: "Generate a QR code PNG from text or URL. Usage: .qr <text>", Execute: cmdQR},
		{Name: "barcode", Category: "tools", Description: "Generate a Code128-style visual barcode PNG. Usage: .barcode <text>", Execute: cmdBarcode},
		{Name: "coascii", Category: "tools", Description: "Convert a replied image into ASCII art.", Execute: cmdCoAscii},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

// EncodeString encodes text using the given codec name.
func EncodeString(codec, text string) (string, error) {
	switch strings.ToLower(codec) {
	case "b64", "base64":
		return base64.StdEncoding.EncodeToString([]byte(text)), nil
	case "b32", "base32":
		return base32.StdEncoding.EncodeToString([]byte(text)), nil
	case "b85", "base85", "ascii85":
		dst := make([]byte, ascii85.MaxEncodedLen(len(text)))
		n := ascii85.Encode(dst, []byte(text))
		return string(dst[:n]), nil
	case "hex":
		return hex.EncodeToString([]byte(text)), nil
	case "bin", "binary":
		parts := make([]string, len(text))
		for i, b := range []byte(text) {
			parts[i] = fmt.Sprintf("%08b", b)
		}
		return strings.Join(parts, " "), nil
	case "oct", "octal":
		parts := make([]string, len(text))
		for i, b := range []byte(text) {
			parts[i] = fmt.Sprintf("%03o", b)
		}
		return strings.Join(parts, " "), nil
	case "rot13":
		return strings.Map(func(r rune) rune {
			switch {
			case r >= 'A' && r <= 'Z':
				return 'A' + (r-'A'+13)%26
			case r >= 'a' && r <= 'z':
				return 'a' + (r-'a'+13)%26
			default:
				return r
			}
		}, text), nil
	case "atbash":
		return strings.Map(func(r rune) rune {
			switch {
			case r >= 'A' && r <= 'Z':
				return 'Z' - (r - 'A')
			case r >= 'a' && r <= 'z':
				return 'z' - (r - 'a')
			default:
				return r
			}
		}, text), nil
	case "morse":
		var out []string
		for _, r := range strings.ToUpper(text) {
			if code, ok := morseEncodeMap[r]; ok {
				out = append(out, code)
			}
		}
		return strings.Join(out, " "), nil
	case "url":
		return url.QueryEscape(text), nil
	case "html":
		return html.EscapeString(text), nil
	default:
		return "", fmt.Errorf("unknown codec %q", codec)
	}
}

// DecodeString decodes text using the given codec name.
func DecodeString(codec, text string) (string, error) {
	switch strings.ToLower(codec) {
	case "b64", "base64":
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
		if err != nil {
			b, err = base64.RawURLEncoding.DecodeString(strings.TrimSpace(text))
		}
		return string(b), err
	case "b32", "base32":
		b, err := base32.StdEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(text)))
		return string(b), err
	case "b85", "base85", "ascii85":
		dst := make([]byte, len(text))
		ndst, _, err := ascii85.Decode(dst, []byte(strings.TrimSpace(text)), true)
		return string(dst[:ndst]), err
	case "hex":
		b, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(text), " ", ""))
		return string(b), err
	case "bin", "binary":
		fields := strings.Fields(text)
		out := make([]byte, 0, len(fields))
		for _, f := range fields {
			v, err := strconv.ParseUint(f, 2, 8)
			if err != nil {
				return "", err
			}
			out = append(out, byte(v))
		}
		return string(out), nil
	case "oct", "octal":
		fields := strings.Fields(text)
		out := make([]byte, 0, len(fields))
		for _, f := range fields {
			v, err := strconv.ParseUint(f, 8, 8)
			if err != nil {
				return "", err
			}
			out = append(out, byte(v))
		}
		return string(out), nil
	case "rot13":
		return EncodeString("rot13", text)
	case "atbash":
		return EncodeString("atbash", text)
	case "morse":
		var b strings.Builder
		for _, tok := range strings.Fields(text) {
			if r, ok := morseDecodeMap[tok]; ok {
				b.WriteRune(r)
			}
		}
		return b.String(), nil
	case "url":
		return url.QueryUnescape(text)
	case "html":
		return html.UnescapeString(text), nil
	case "jwt":
		return decodeJWTString(text)
	default:
		return "", fmt.Errorf("unknown codec %q", codec)
	}
}

func decodeJWTString(token string) (string, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid JWT (expected header.payload.sig)")
	}
	hdr, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	pay, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var hObj, pObj any
	_ = json.Unmarshal(hdr, &hObj)
	_ = json.Unmarshal(pay, &pObj)
	hb, _ := json.MarshalIndent(hObj, "", "  ")
	pb, _ := json.MarshalIndent(pObj, "", "  ")
	return fmt.Sprintf("*Header:*\n```json\n%s\n```\n*Payload:*\n```json\n%s\n```", string(hb), string(pb)), nil
}

func cmdEncode(client *whatsmeow.Client, msg *events.Message, args []string) {
	parts := strings.SplitN(strings.TrimSpace(strings.Join(args, " ")), " ", 2)
	if len(parts) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.encode <b64|b32|b85|hex|bin|oct|rot13|atbash|morse|url|html> <text>`")
		return
	}
	out, err := EncodeString(parts[0], parts[1])
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, out)
}

func cmdDecode(client *whatsmeow.Client, msg *events.Message, args []string) {
	parts := strings.SplitN(strings.TrimSpace(strings.Join(args, " ")), " ", 2)
	if len(parts) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.decode <b64|b32|b85|hex|bin|oct|rot13|atbash|morse|url|html|jwt> <text>`")
		return
	}
	out, err := DecodeString(parts[0], parts[1])
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Decode failed: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, out)
}

func cmdCodecShortcut(codec string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		raw := strings.TrimSpace(strings.Join(args, " "))
		if raw == "" {
			if q := utils.ExtractQuotedOrOwnMessage(msg); q != nil {
				raw = strings.TrimSpace(utils.GetTextFromMessage(q))
			}
		}
		if raw == "" {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Usage: `.%s [enc|dec] <text>`", codec))
			return
		}
		mode := "enc"
		payload := raw
		parts := strings.SplitN(raw, " ", 2)
		if len(parts) == 2 && (strings.EqualFold(parts[0], "dec") || strings.EqualFold(parts[0], "decode") || strings.EqualFold(parts[0], "-d")) {
			mode = "dec"
			payload = parts[1]
		} else if len(parts) == 2 && (strings.EqualFold(parts[0], "enc") || strings.EqualFold(parts[0], "encode") || strings.EqualFold(parts[0], "-e")) {
			payload = parts[1]
		}
		var (
			out string
			err error
		)
		if mode == "dec" {
			out, err = DecodeString(codec, payload)
		} else {
			out, err = EncodeString(codec, payload)
		}
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ "+err.Error())
			return
		}
		utils.SendReply(client, msg.Info.Chat, out)
	}
}

func cmdJWT(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.jwt <token>`")
		return
	}
	out, err := decodeJWTString(raw)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, out)
}

func cmdTextTransform(mode string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		raw := strings.TrimSpace(strings.Join(args, " "))
		if raw == "" {
			if q := utils.ExtractQuotedOrOwnMessage(msg); q != nil {
				raw = strings.TrimSpace(utils.GetTextFromMessage(q))
			}
		}
		if raw == "" {
			utils.SendReply(client, msg.Info.Chat, "Usage: `."+mode+" <text>`")
			return
		}
		switch mode {
		case "upper":
			utils.SendReply(client, msg.Info.Chat, strings.ToUpper(raw))
		case "lower":
			utils.SendReply(client, msg.Info.Chat, strings.ToLower(raw))
		case "reverse":
			runes := []rune(raw)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			utils.SendReply(client, msg.Info.Chat, string(runes))
		}
	}
}

func cmdLorem(client *whatsmeow.Client, msg *events.Message, args []string) {
	n := 1
	if len(args) > 0 {
		if v, err := strconv.Atoi(strings.TrimSpace(args[0])); err == nil && v >= 1 && v <= 5 {
			n = v
		}
	}
	para := "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat."
	parts := make([]string, n)
	for i := range parts {
		parts[i] = para
	}
	utils.SendReply(client, msg.Info.Chat, strings.Join(parts, "\n\n"))
}

func cmdReadMore(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	parts := strings.SplitN(raw, "|", 2)
	if len(parts) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.readmore visible text|hidden text`")
		return
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(parts[0])+strings.Repeat("\u200e", 4001)+"\n"+strings.TrimSpace(parts[1]))
}

// EvaluateMath safely evaluates arithmetic expressions supporting + - * / ^ % ( ) and decimals.
func EvaluateMath(expr string) (float64, error) {
	p := &mathParser{s: strings.ReplaceAll(strings.TrimSpace(expr), " ", "")}
	v, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	if p.pos < len(p.s) {
		return 0, fmt.Errorf("unexpected character %q", p.s[p.pos])
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("result is undefined")
	}
	return v, nil
}

type mathParser struct {
	s   string
	pos int
}

func (p *mathParser) parseExpr() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for p.pos < len(p.s) && (p.s[p.pos] == '+' || p.s[p.pos] == '-') {
		op := p.s[p.pos]
		p.pos++
		right, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}
	return left, nil
}

func (p *mathParser) parseTerm() (float64, error) {
	left, err := p.parsePower()
	if err != nil {
		return 0, err
	}
	for p.pos < len(p.s) && (p.s[p.pos] == '*' || p.s[p.pos] == '/' || p.s[p.pos] == '%') {
		op := p.s[p.pos]
		p.pos++
		right, err := p.parsePower()
		if err != nil {
			return 0, err
		}
		switch op {
		case '*':
			left *= right
		case '/':
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			left /= right
		case '%':
			if right == 0 {
				return 0, fmt.Errorf("modulo by zero")
			}
			left = math.Mod(left, right)
		}
	}
	return left, nil
}

func (p *mathParser) parsePower() (float64, error) {
	base, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	if p.pos < len(p.s) && p.s[p.pos] == '^' {
		p.pos++
		exp, err := p.parsePower()
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}
	return base, nil
}

func (p *mathParser) parseUnary() (float64, error) {
	if p.pos < len(p.s) && p.s[p.pos] == '-' {
		p.pos++
		v, err := p.parseUnary()
		return -v, err
	}
	if p.pos < len(p.s) && p.s[p.pos] == '+' {
		p.pos++
		return p.parseUnary()
	}
	return p.parsePrimary()
}

func (p *mathParser) parsePrimary() (float64, error) {
	if p.pos >= len(p.s) {
		return 0, fmt.Errorf("unexpected end of expression")
	}
	if p.s[p.pos] == '(' {
		p.pos++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.pos >= len(p.s) || p.s[p.pos] != ')' {
			return 0, fmt.Errorf("missing closing ')'")
		}
		p.pos++
		return v, nil
	}
	start := p.pos
	for p.pos < len(p.s) && (unicode.IsDigit(rune(p.s[p.pos])) || p.s[p.pos] == '.') {
		p.pos++
	}
	if start == p.pos {
		return 0, fmt.Errorf("expected number at position %d", p.pos)
	}
	return strconv.ParseFloat(p.s[start:p.pos], 64)
}

func cmdCalculator(client *whatsmeow.Client, msg *events.Message, args []string) {
	expr := strings.TrimSpace(strings.Join(args, " "))
	if expr == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.calc (12 + 8) * 3 / 4`")
		return
	}
	val, err := EvaluateMath(expr)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🧮 `%s` = *%g*", expr, val))
}

func cmdColor(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimPrefix(strings.TrimSpace(strings.Join(args, " ")), "#")
	if len(raw) == 3 {
		raw = string([]byte{raw[0], raw[0], raw[1], raw[1], raw[2], raw[2]})
	}
	if len(raw) != 6 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.color #00a884`")
		return
	}
	rgb, err := hex.DecodeString(raw)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Invalid hex color.")
		return
	}
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	col := color.RGBA{rgb[0], rgb[1], rgb[2], 255}
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.SetRGBA(x, y, col)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	caption := fmt.Sprintf("🎨 *#%s*\n• RGB: `rgb(%d, %d, %d)`", strings.ToUpper(raw), rgb[0], rgb[1], rgb[2])
	_, _ = utils.SendImageData(context.Background(), client, msg.Info.Chat, buf.Bytes(), "image/png", caption)
}

func cmdQR(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.qr <text or URL>`")
		return
	}
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ QR generation failed: "+err.Error())
		return
	}
	_, _ = utils.SendImageData(context.Background(), client, msg.Info.Chat, code.PNG(), "image/png", "📱 QR Code")
}

// RenderBarcodePNG creates a deterministic visual barcode PNG for text.
func RenderBarcodePNG(text string) []byte {
	const barW, h, pad = 3, 120, 20
	bits := make([]bool, 0, len(text)*11+24)
	// Start guard
	bits = append(bits, true, true, false, true, false, false, true, false)
	for _, b := range []byte(text) {
		bits = append(bits, true)
		for i := 7; i >= 0; i-- {
			bits = append(bits, (b>>uint(i))&1 == 1)
		}
		bits = append(bits, false)
	}
	// Stop guard
	bits = append(bits, true, true, false, true, true, false, true)

	w := pad*2 + len(bits)*barW
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	for i, on := range bits {
		if !on {
			continue
		}
		x0 := pad + i*barW
		for x := x0; x < x0+barW; x++ {
			for y := 14; y < h-14; y++ {
				img.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func cmdBarcode(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.barcode <text>`")
		return
	}
	if len(text) > 48 {
		text = text[:48]
	}
	_, _ = utils.SendImageData(context.Background(), client, msg.Info.Chat, RenderBarcodePNG(text), "image/png", "🏷️ Barcode: `"+text+"`")
}

func cmdCoAscii(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || target.GetImageMessage() == nil {
		utils.SendReply(client, msg.Info.Chat, "📷 Reply to an image with `.coascii`.")
		return
	}
	data, _, _, err := utils.DownloadMedia(context.Background(), client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to download image.")
		return
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Unsupported image format.")
		return
	}
	const ramp = " .:-=+*#%@"
	b := img.Bounds()
	cols, rows := 44, 22
	var out strings.Builder
	out.WriteString("```\n")
	for r := 0; r < rows; r++ {
		y := b.Min.Y + (r*b.Dy())/rows
		for c := 0; c < cols; c++ {
			x := b.Min.X + (c*b.Dx())/cols
			cr, cg, cb, _ := img.At(x, y).RGBA()
			lum := (299*cr + 587*cg + 114*cb) / 1000
			idx := int(lum) * (len(ramp) - 1) / 65535
			out.WriteByte(ramp[idx])
		}
		out.WriteByte('\n')
	}
	out.WriteString("```")
	utils.SendReply(client, msg.Info.Chat, out.String())
}
