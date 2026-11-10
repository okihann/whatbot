package utils

// Blank imports register image decoders for image.Decode: PNG and GIF are
// pulled in by image/png and image/gif, but JPEG and WebP must be imported
// explicitly. Callers (meme.go, /get) rely on all four.
import (
	_ "image/gif"
	_ "image/jpeg"

	_ "golang.org/x/image/webp"
)
