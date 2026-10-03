package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakePlayer makes a Player whose "ffplay" is a script that logs each file it
// is asked to play, then sleeps for $FAKE_SLEEP seconds.
func fakePlayer(t *testing.T, sleep string) (*Player, string) {
	t.Helper()
	dir := t.TempDir()
	logf := filepath.Join(dir, "played.log")
	script := filepath.Join(dir, "ffplay")
	body := "#!/bin/sh\nfor a; do f=\"$a\"; done\necho \"$1 $2 $(basename \"$f\")\" >> " + logf + "\nsleep " + sleep + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLASHBACK_PLAYER", script)
	p := NewPlayer("test")
	if p == nil {
		t.Fatal("no player")
	}
	return p, logf
}

func playedLines(logf string) []string {
	b, _ := os.ReadFile(logf)
	return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(b)), "\n", " | "))
}

func waitFor(cond func() bool) bool {
	for i := 0; i < 100; i++ {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

type memMedia map[string][]byte

func (m memMedia) ReadMedia(name string) ([]byte, error) {
	if b, ok := m[name]; ok {
		return b, nil
	}
	return nil, os.ErrNotExist
}

func TestPlayerPlaysInOrderAndStops(t *testing.T) {
	p, logf := fakePlayer(t, "0")
	media := memMedia{"a.mp3": []byte("A"), "b.ogg": []byte("B")}
	p.Play(media, []string{"a.mp3", "b.ogg"})
	if !waitFor(func() bool { b, _ := os.ReadFile(logf); return strings.Count(string(b), "\n") == 2 }) {
		t.Fatalf("both sounds weren't played: %q", playedLines(logf))
	}
	b, _ := os.ReadFile(logf)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if !strings.HasPrefix(lines[0], "-nodisp -autoexit") || !strings.HasSuffix(lines[0], ".mp3") || !strings.HasSuffix(lines[1], ".ogg") {
		t.Fatalf("played %q", lines)
	}

	// a long sound is cut off by Stop, and the next one in the list never starts
	p2, logf2 := fakePlayer(t, "30")
	p2.Play(media, []string{"a.mp3", "b.ogg"})
	if !waitFor(func() bool { p2.mu.Lock(); defer p2.mu.Unlock(); return p2.proc != nil }) {
		t.Fatal("player never started")
	}
	p2.mu.Lock()
	proc := p2.proc
	p2.mu.Unlock()
	p2.Stop()
	if !waitFor(func() bool { return proc.Signal(syscall.Signal(0)) != nil }) {
		t.Fatal("the stopped sound kept playing")
	}
	time.Sleep(200 * time.Millisecond)
	if b, _ := os.ReadFile(logf2); strings.Contains(string(b), ".ogg") {
		t.Fatalf("the next sound started after Stop: %q", b)
	}
}

func TestSoundsFollowTheCard(t *testing.T) {
	u, env := newTestUI(t, "legacy.apkg")
	p, logf := fakePlayer(t, "0")
	env.Player = p
	c := NewCanvas(640, 480)
	press(u, c, BtnA) // open the file
	pkg := u.cur.Pkg
	ci := -1
	for i := range pkg.Cards {
		if strings.Contains(pkg.CardHTML(i, true), "pan.mp3") && !strings.Contains(pkg.CardHTML(i, false), "pan.mp3") {
			ci = i
			break
		}
	}
	if ci < 0 {
		t.Fatal("no card with a sound on its answer")
	}
	count := func() int { b, _ := os.ReadFile(logf); return strings.Count(string(b), "\n") }
	settle := func(n int) bool {
		return waitFor(func() bool { return count() == n }) && func() bool { time.Sleep(100 * time.Millisecond); return count() == n }()
	}

	u.scr = scrView
	u.viewList, u.viewPos = []int{ci}, 0
	u.showCard(ci, false)
	if !settle(0) {
		t.Fatalf("question without sound played something")
	}
	u.flip()
	if !settle(1) {
		t.Fatalf("answer's sound not played on flip (%d)", count())
	}
	u.cycleTextSize() // same side: no replay
	if !settle(1) {
		t.Fatalf("changing text size replayed the sound")
	}
	u.frame += flipFrames + 1
	u.Key(BtnStart)
	if !settle(2) {
		t.Fatalf("START didn't replay (%d)", count())
	}
	env.Config.AutoplayAudio = false
	u.showCard(ci, false)
	u.flip()
	if !settle(2) {
		t.Fatalf("autoplay off still played")
	}
}
