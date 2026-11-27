package gamecmd

import (
	"bytes"
	"testing"
)

func TestGameEngines(t *testing.T) {
	if got := ScoreWordle("APPLE", "APPLE"); got != "🟩🟩🟩🟩🟩" {
		t.Fatalf("expected all green wordle score, got %q", got)
	}
	if got := ScoreWordle("APPLE", "PLEAP"); got != "🟨🟨🟨🟨🟨" {
		t.Fatalf("expected all yellow wordle score, got %q", got)
	}

	board := [9]string{"X", "X", "X", "O", "O", "", "", "", ""}
	if w := CheckTTTWinner(board); w != "X" {
		t.Fatalf("expected X winner, got %q", w)
	}

	pngBytes := RenderChessBoardPNG(initChessBoard())
	if !bytes.HasPrefix(pngBytes, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("expected valid PNG chess board")
	}
}
