package minigame

import "testing"

func TestSlide2048Row(t *testing.T) {
	got, gained := Slide2048Row([4]int{2, 2, 4, 4})
	want := [4]int{4, 8, 0, 0}
	if got != want || gained != 12 {
		t.Fatalf("expected (%v, 12), got (%v, %d)", want, got, gained)
	}
}
