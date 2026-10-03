package main

import (
	"testing"
	"time"
)

// pump runs the UI goroutine's queued work, as the main loop would, until
// done reports true or the time runs out.
func pump(u *UI, done func() bool, wait time.Duration) bool {
	deadline := time.After(wait)
	for !done() {
		select {
		case f := <-u.post:
			f()
		case <-deadline:
			return false
		}
	}
	return true
}

// TestPrepareNext studies a deck with the main loop running, so the next side
// is prepared on another goroutine (run it with -race), and checks that the
// prepared sides are the ones shown and match a fresh layout.
func TestPrepareNext(t *testing.T) {
	u, _ := newTestUI(t, "new.apkg")
	c := NewCanvas(640, 480)
	u.Draw(c)
	press(u, c, BtnA, BtnA) // open the file, then study all its decks
	if u.scr != scrStudy || u.answer {
		t.Fatalf("expected study front, scr=%v", u.scr)
	}
	used := 0
	for i := 0; i < 20 && u.scr == scrStudy; i++ {
		// the answer of the card on screen gets prepared
		if !pump(u, func() bool { return u.pre.layout != nil }, 5*time.Second) {
			t.Fatalf("card %d: answer never prepared", u.card)
		}
		if u.pre.ci != u.card || !u.pre.answer {
			t.Fatalf("prepared %d/%v while showing %d front", u.pre.ci, u.pre.answer, u.card)
		}
		fresh := u.layoutHTML(u.cur.Pkg.CardHTML(u.card, true))
		if fresh.H != u.pre.layout.H || len(fresh.Lines) != len(u.pre.layout.Lines) {
			t.Fatalf("card %d: prepared answer differs from a fresh layout", u.card)
		}
		want := u.pre.layout
		press(u, c, BtnA) // flip
		if u.layout == want {
			used++
		}
		// then the next card's question
		next := u.sess.Upcoming(u.card, u.now())
		if next >= 0 {
			if !pump(u, func() bool { return u.pre.layout != nil }, 5*time.Second) {
				t.Fatalf("card %d: next question never prepared", next)
			}
			if u.pre.ci != next || u.pre.answer {
				t.Fatalf("prepared %d/%v, expected question of %d", u.pre.ci, u.pre.answer, next)
			}
		}
		want = u.pre.layout
		press(u, c, BtnA) // good
		if u.scr == scrStudy && u.card == next && u.layout == want {
			used++
		}
	}
	if used < 10 {
		t.Errorf("prepared sides used %d times, expected most flips and answers to use them", used)
	}
}
