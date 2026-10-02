package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestUI sets up a UI over a temporary deck folder holding copies of the test packages.
func newTestUI(t *testing.T, decks ...string) (*UI, *Env) {
	t.Helper()
	dir := t.TempDir()
	folder := filepath.Join(dir, "Anki")
	os.MkdirAll(folder, 0o755)
	for _, d := range decks {
		b, err := os.ReadFile("testdata/" + d)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(folder, d), b, 0o644)
	}
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local)
	env := &Env{AppDir: dir, DataDir: filepath.Join(dir, "data"), Now: func() time.Time { return now }}
	env.Config = defaultConfig()
	env.Config.DeckFolders = []string{folder}
	loadCardFonts("")
	u := NewUI(env, 640, 480)
	u.Start()
	// run the background unpacking to completion
	deadline := time.After(20 * time.Second)
	for u.scanning {
		select {
		case f := <-u.post:
			f()
		case <-deadline:
			t.Fatal("unpacking never finished")
		}
	}
	return u, env
}

func press(u *UI, c *Canvas, keys ...int) {
	for _, k := range keys {
		u.Key(k)
		u.frame += flipFrames + 1 // let any flip animation finish
		u.Draw(c)
	}
}

func TestUIFlow(t *testing.T) {
	u, env := newTestUI(t, "new.apkg", "genanki.apkg")
	c := NewCanvas(640, 480)
	if len(u.lib) != 2 || u.lib[0].Pkg == nil || u.lib[1].Pkg == nil {
		t.Fatalf("library = %+v", u.lib)
	}
	u.Draw(c)
	// genanki.apkg sorts first; pick new.apkg
	press(u, c, BtnDown, BtnA)
	if u.scr != scrDecks || len(u.decks) != 5 {
		t.Fatalf("decks screen: scr=%v decks=%d", u.scr, len(u.decks))
	}
	// row 0 = all decks, 1 Science, 2 Spanish Basics, 3 Food, 4 日本語
	press(u, c, BtnDown, BtnDown, BtnA)
	if u.scr != scrStudy || u.answer {
		t.Fatalf("expected study front, scr=%v", u.scr)
	}
	// study the whole session: A flips, then answer Good; learning cards come back
	for i := 0; i < 60 && u.scr == scrStudy; i++ {
		press(u, c, BtnA) // show answer
		if !u.answer {
			t.Fatalf("flip didn't show the answer")
		}
		press(u, c, BtnA) // good
		env.Now = func(t0 time.Time) func() time.Time {
			return func() time.Time { return t0.Add(11 * time.Minute) }
		}(env.Now())
	}
	if u.scr != scrDone {
		t.Fatalf("session didn't finish, scr=%v", u.scr)
	}
	prog := u.cur.Prog
	if prog.NewToday != 7 || len(prog.Cards) != 7 {
		t.Fatalf("progress: new today %d, cards %d", prog.NewToday, len(prog.Cards))
	}
	// progress was saved to disk
	if _, err := os.Stat(prog.path); err != nil {
		t.Fatalf("progress not saved: %v", err)
	}
	// Done → shuffle through, then back
	press(u, c, BtnA)
	if u.scr != scrView || !u.shuffled || len(u.viewList) != 9 {
		t.Fatalf("shuffle: scr=%v list=%d", u.scr, len(u.viewList))
	}
	press(u, c, BtnA, BtnA, BtnRight, BtnLeft, BtnY, BtnSelect, BtnB)
	if u.scr != scrDecks {
		t.Fatalf("back from shuffle: scr=%v", u.scr)
	}
	// browse Science and open a card
	press(u, c, BtnUp, BtnX)
	if u.scr != scrBrowse || len(u.list) != 47 {
		t.Fatalf("browse: scr=%v list=%d", u.scr, len(u.list))
	}
	press(u, c, BtnR1, BtnA)
	if u.scr != scrView || u.card != u.list[u.listSel] {
		t.Fatalf("view from browse failed")
	}
	press(u, c, BtnX, BtnDown, BtnDown, BtnUp, BtnRight, BtnB)
	if u.scr != scrBrowse {
		t.Fatalf("back to browse: scr=%v", u.scr)
	}
	press(u, c, BtnB, BtnB)
	if u.scr != scrLibrary {
		t.Fatalf("back to library: scr=%v", u.scr)
	}
	// the library counts reflect today's studying
	if n := u.lib[1].Counts.New; n != 13 {
		t.Errorf("library new count after study = %d, want 13", n)
	}
	press(u, c, BtnMenu)
	if !u.quit {
		t.Fatal("menu should quit")
	}
	u.saveAll()
}

func TestUIEmptyAndBroken(t *testing.T) {
	u, _ := newTestUI(t)
	c := NewCanvas(640, 480)
	u.Draw(c)
	if len(u.lib) != 0 {
		t.Fatal("expected empty library")
	}
	// a file that isn't a real package shows an error row, not a crash
	dir := u.env.Config.DeckFolders[0]
	os.WriteFile(filepath.Join(dir, "broken.apkg"), []byte("not a zip"), 0o644)
	press(u, c, BtnSelect)
	for u.scanning {
		(<-u.post)()
	}
	if len(u.lib) != 1 || u.lib[0].Err == "" {
		t.Fatalf("broken file: %+v", u.lib)
	}
	press(u, c, BtnA)
	if u.modalTitle == "" {
		t.Fatal("expected an error message for the broken file")
	}
	press(u, c, BtnA)
	if u.modalTitle != "" {
		t.Fatal("message should close")
	}
}

func TestUISmallScreenAndTextSizes(t *testing.T) {
	u, _ := newTestUI(t, "ug_new.apkg")
	if len(u.lib) == 0 || u.lib[0].Pkg == nil {
		t.Skip("real deck not present")
	}
	c := NewCanvas(640, 480)
	press(u, c, BtnA, BtnA)
	for _, size := range []int{18, 22, 26, 30} {
		u.env.Config.TextSize = size
		for i := 0; i < 20 && u.scr == scrStudy; i++ {
			press(u, c, BtnA, BtnDown, BtnDown, BtnY)
		}
	}
}

// TestScreenShowsAnswerAfterFlip runs the device's draw loop: draw on keys, and
// on ticks only when TickNeedsDraw says so. After the flip finishes, what's on
// screen must be the answer side, not the last frame of the animation.
func TestScreenShowsAnswerAfterFlip(t *testing.T) {
	u, _ := newTestUI(t, "new.apkg")
	screen := NewCanvas(640, 480)
	key := func(k int) {
		u.Key(k)
		u.Draw(screen)
	}
	tick := func(n int) {
		for i := 0; i < n; i++ {
			u.frame++
			u.Tick()
			if u.TickNeedsDraw() {
				u.Draw(screen)
			}
		}
	}
	key(BtnA) // open the deck file
	key(BtnA) // study
	tick(3)
	key(BtnA) // show answer
	tick(flipFrames + 5)
	if !u.answer {
		t.Fatal("not on the answer side")
	}
	fresh := NewCanvas(640, 480)
	u.Draw(fresh)
	if string(fresh.Pix) != string(screen.Pix) {
		t.Fatal("screen still shows a stale frame after the flip finished")
	}
	// a toast disappears from the screen once it expires
	u.showToast("HELLO")
	u.Draw(screen)
	tick(1) // on the device a toast lives for many ticks
	u.toastUntil = time.Now().Add(-time.Second)
	tick(2)
	u.Draw(fresh)
	if string(fresh.Pix) != string(screen.Pix) {
		t.Fatal("expired toast left on screen")
	}
}
