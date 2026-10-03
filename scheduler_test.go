package main

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSchedulerFlow(t *testing.T) {
	p := openTest(t, "new.apkg")
	prog := LoadProgress(t.TempDir(), "x.apkg", time.Now())
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local)
	prog.rollover(now)
	idx := p.CardsIn("Spanish Basics")
	s := NewSession(p, prog, idx, 20, 200, now)
	// 9 cards in 7 notes: siblings are buried, so 7 today
	if s.Total != 7 {
		t.Fatalf("session total %d, want 7", s.Total)
	}
	ci := s.Next(now)
	id := p.Cards[ci].ID
	if l := prog.IntervalLabel(id, answerAgain, now); l != "1m" {
		t.Errorf("again label %q", l)
	}
	if l := prog.IntervalLabel(id, answerGood, now); l != "10m" {
		t.Errorf("good label %q", l)
	}
	// Easy skips the steps: FSRS's first Easy stability is 8.3 days, fuzzed to 6–10
	if l := prog.IntervalLabel(id, answerEasy, now); !strings.HasSuffix(l, "d") {
		t.Errorf("easy label %q", l)
	} else if d, _ := strconv.Atoi(strings.TrimSuffix(l, "d")); d < 6 || d > 10 {
		t.Errorf("easy label %q", l)
	}
	s.Answer(ci, answerGood, now) // -> 10 min step
	// the rest are new; answer Easy for all of them
	for {
		c := s.Next(now)
		if c < 0 || c == ci {
			break
		}
		s.Answer(c, answerEasy, now)
	}
	// only the learning card is left, shown early
	if c := s.Next(now); c != ci {
		t.Fatalf("expected learning card back, got %d", c)
	}
	later := now.Add(11 * time.Minute)
	s.Answer(ci, answerGood, later)
	if s.Next(later) != -1 || s.Done != 7 {
		t.Fatalf("session not finished: next=%d done=%d", s.Next(later), s.Done)
	}
	// Good, Good on the first day: stability 2.3 days, so back in 2 days
	st := prog.State(id)
	if st.Ivl != 2 || st.Due != prog.Day+2 || st.Learn != 0 || st.S < 2.2 || st.S > 2.4 {
		t.Fatalf("graduated state %+v", st)
	}
	tomorrow := now.Add(24 * time.Hour)
	prog.rollover(tomorrow)
	if c := prog.Count(p, idx, tomorrow); c.Due != 0 || c.New != 2 {
		t.Fatalf("tomorrow counts %+v", c)
	}
	later = now.Add(48 * time.Hour)
	prog.rollover(later)
	if c := prog.Count(p, idx, later); c.Due != 1 {
		t.Fatalf("day-after counts %+v", c)
	}
	// remembered on time: the interval grows
	if next := prog.Next(id, answerGood, later); next.Ivl <= 2 || next.S <= st.S {
		t.Errorf("review good: %+v", next)
	}
	prog.Answer(id, answerAgain, later)
	if st := prog.State(id); st.Learn != 2 || st.Lapses != 1 || st.S >= 2.3 {
		t.Fatalf("lapse state %+v", st)
	}
	if err := prog.Save(); err != nil {
		t.Fatal(err)
	}
	again := LoadProgress(filepathDir(prog.path), "x.apkg", later)
	if st := again.State(id); st == nil || st.Lapses != 1 || again.Studied != prog.Studied {
		t.Fatalf("reloaded progress %+v", st)
	}
}

func filepathDir(p string) string {
	// progress lives in <data>/progress/<hash>.json
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			p = p[:i]
			break
		}
	}
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return p
}
