package main

import (
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"
)

// TestFSRSMatchesReference replays review histories made by the reference
// implementation (py-fsrs 6.3.2, fuzz off; see testdata/fsrs_golden.json) and
// checks stability, difficulty, state and the next interval after every answer.
func TestFSRSMatchesReference(t *testing.T) {
	b, err := os.ReadFile("testdata/fsrs_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Retention float64 `json:"retention"`
		Sequences [][]struct {
			T     int64   `json:"t"`
			G     int     `json:"g"`
			S     float64 `json:"s"`
			D     float64 `json:"d"`
			State int     `json:"state"`
			Step  int     `json:"step"`
			Due   int64   `json:"due"`
		} `json:"sequences"`
	}
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	saved := sched
	defer func() { sched = saved }()
	sched = NewScheduler(nil, g.Retention, 36500, false)
	sched.noFuzz = true
	close := func(a, b float64) bool { return math.Abs(a-b) <= 1e-4*math.Max(1, math.Abs(b)) }
	checked := 0
	for n, seq := range g.Sequences {
		p := &Progress{Cards: map[string]*CardState{}}
		for k, want := range seq {
			now := time.Unix(want.T, 0).UTC()
			p.Answer(1, want.G, now)
			st := p.State(1)
			learn := map[int]int{1: 1, 2: 0, 3: 2}[want.State]
			if st.Learn != learn || !close(st.S, want.S) || !close(st.D, want.D) {
				t.Fatalf("sequence %d answer %d (rating %d): got learn=%d S=%.5f D=%.5f, want learn=%d S=%.5f D=%.5f",
					n, k, want.G, st.Learn, st.S, st.D, learn, want.S, want.D)
			}
			if learn == 0 {
				if days := int((want.Due - want.T) / 86400); st.Ivl != days {
					t.Fatalf("sequence %d answer %d: interval %d days, want %d", n, k, st.Ivl, days)
				}
			} else if st.LearnAt-want.T != want.Due-want.T || st.Step != want.Step {
				t.Fatalf("sequence %d answer %d: back in %ds at step %d, want %ds at step %d", n, k, st.LearnAt-want.T, st.Step, want.Due-want.T, want.Step)
			}
			checked++
		}
	}
	t.Logf("%d answers in %d histories match py-fsrs", checked, len(g.Sequences))
}

// TestFuzzStaysInRange checks the fuzzed intervals stay within Anki's bounds
// and actually spread out.
func TestFuzzStaysInRange(t *testing.T) {
	s := NewScheduler(nil, 0.9, 36500, false)
	seen := map[int]bool{}
	for id := int64(0); id < 2000; id++ {
		f := s.fuzz(100, id, 3)
		if f < 93 || f > 107 {
			t.Fatalf("fuzz(100) = %d", f)
		}
		seen[f] = true
		if s.fuzz(2, id, 3) != 2 {
			t.Fatal("short intervals must not be fuzzed")
		}
	}
	if len(seen) < 10 {
		t.Fatalf("fuzz barely spreads: %v", seen)
	}
}

// TestSM2Migration converts a card studied with the old scheduler.
func TestSM2Migration(t *testing.T) {
	saved := sched
	defer func() { sched = saved }()
	sched = NewScheduler(nil, 0.9, 36500, false)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	today := dayNumber(now)
	p := &Progress{Cards: map[string]*CardState{"7": {Ivl: 20, Ease: 2500, Due: today, Reps: 5}}, Day: today}
	m := migrateSM2(*p.State(7))
	if m.S < 15 || m.S > 25 || m.D < 1 || m.D > 10 || m.LastDay != today-20 {
		t.Fatalf("migrated %+v", m)
	}
	// a card reviewed on time with Good grows well past its old interval
	p.Answer(7, answerGood, now)
	if st := p.State(7); st.Ivl < 30 || st.S <= m.S {
		t.Fatalf("after Good: %+v", st)
	}
}
