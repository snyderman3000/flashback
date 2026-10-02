package main

// Spaced repetition, Anki-style but simplified (SM-2 with learning steps).
//
// New cards go through two short learning steps (1 min, 10 min) and then
// graduate to a 1-day interval (Easy skips straight to 4 days). Review cards
// grow by their ease factor; Again sends them back through a 10-minute
// relearning step and lowers the ease. Days roll over at 4 a.m. local time.
//
// Progress is stored per package file in data/progress/<hash>.json, keyed by
// Anki's card ids, so replacing a deck with an updated export keeps progress.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

type CardState struct {
	Ivl     int   `json:"i,omitempty"` // days
	Ease    int   `json:"e,omitempty"` // permille, 2500 = 250%
	Due     int   `json:"d,omitempty"` // day number, for review cards
	Reps    int   `json:"r,omitempty"`
	Lapses  int   `json:"l,omitempty"`
	Learn   int   `json:"ln,omitempty"` // 0 review, 1 learning, 2 relearning
	Step    int   `json:"s,omitempty"`
	LearnAt int64 `json:"t,omitempty"` // unix seconds when a learning card is due
}

type Progress struct {
	Cards    map[string]*CardState `json:"cards"`
	Day      int                   `json:"day"`
	NewToday int                   `json:"new_today"`
	RevToday int                   `json:"rev_today"`
	Studied  int                   `json:"studied_total"`
	path     string
	dirty    int
}

const (
	answerAgain = 1
	answerGood  = 3
	answerEasy  = 4
)

var learnSteps = []int64{60, 600} // seconds
const relearnStep = 600

// dayNumber counts days since the epoch in local time, rolling over at 4 a.m.
func dayNumber(t time.Time) int {
	_, off := t.Zone()
	return int(math.Floor(float64(t.Unix()+int64(off)-4*3600) / 86400))
}

func progressPath(dataDir, pkgPath string) string {
	h := sha1.Sum([]byte(filepath.Base(pkgPath)))
	return filepath.Join(dataDir, "progress", hex.EncodeToString(h[:6])+".json")
}

func LoadProgress(dataDir, pkgPath string, now time.Time) *Progress {
	p := &Progress{Cards: map[string]*CardState{}, path: progressPath(dataDir, pkgPath)}
	if b, err := os.ReadFile(p.path); err == nil {
		json.Unmarshal(b, p)
		if p.Cards == nil {
			p.Cards = map[string]*CardState{}
		}
	}
	p.rollover(now)
	return p
}

func (p *Progress) rollover(now time.Time) {
	if d := dayNumber(now); d != p.Day {
		p.Day, p.NewToday, p.RevToday = d, 0, 0
	}
}

func (p *Progress) Save() error {
	if p.path == "" {
		return nil
	}
	os.MkdirAll(filepath.Dir(p.path), 0o755)
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	p.dirty = 0
	return os.Rename(tmp, p.path)
}

func (p *Progress) State(id int64) *CardState {
	return p.Cards[strconv.FormatInt(id, 10)]
}

// Kind of a card right now.
const (
	kindNew = iota
	kindLearn
	kindDue
	kindLater
)

func (p *Progress) kind(id int64, now time.Time) int {
	st := p.State(id)
	if st == nil {
		return kindNew
	}
	if st.Learn > 0 {
		// learning cards due within the next 20 minutes count as due today
		if st.LearnAt <= now.Unix()+20*60 || dayNumber(time.Unix(st.LearnAt, 0)) <= p.Day {
			return kindLearn
		}
		return kindLater
	}
	if st.Due <= p.Day {
		return kindDue
	}
	return kindLater
}

// Counts summarises a set of cards.
type Counts struct {
	New, Learn, Due, Total int
}

func (p *Progress) Count(pk *Package, idx []int, now time.Time) Counts {
	var c Counts
	c.Total = len(idx)
	for _, i := range idx {
		switch p.kind(pk.Cards[i].ID, now) {
		case kindNew:
			c.New++
		case kindLearn:
			c.Learn++
		case kindDue:
			c.Due++
		}
	}
	return c
}

// Next returns the state after answering, without changing anything.
func (p *Progress) Next(id int64, ans int, now time.Time) CardState {
	var st CardState
	if cur := p.State(id); cur != nil {
		st = *cur
	} else {
		st = CardState{Ease: 2500, Learn: 1}
	}
	if st.Ease == 0 {
		st.Ease = 2500
	}
	t := now.Unix()
	switch {
	case st.Learn == 1: // new or learning
		switch ans {
		case answerAgain:
			st.Step = 0
			st.LearnAt = t + learnSteps[0]
		case answerGood:
			st.Step++
			if st.Step >= len(learnSteps) {
				st = graduate(st, p.Day, 1)
			} else {
				st.LearnAt = t + learnSteps[st.Step]
			}
		case answerEasy:
			st = graduate(st, p.Day, 4)
		}
	case st.Learn == 2: // relearning after a lapse
		switch ans {
		case answerAgain:
			st.LearnAt = t + relearnStep
		default:
			st = graduate(st, p.Day, st.Ivl)
		}
	default: // review
		switch ans {
		case answerAgain:
			st.Lapses++
			st.Ease = max(1300, st.Ease-200)
			st.Ivl = max(1, st.Ivl/2)
			st.Learn, st.Step, st.LearnAt = 2, 0, t+relearnStep
		case answerGood:
			late := max(0, p.Day-st.Due)
			n := int(math.Round(float64(st.Ivl+late/2) * float64(st.Ease) / 1000))
			st.Ivl = min(36500, max(st.Ivl+1, n))
			st.Due = p.Day + st.Ivl
		case answerEasy:
			late := max(0, p.Day-st.Due)
			n := int(math.Round(float64(st.Ivl+late) * float64(st.Ease) / 1000 * 1.3))
			st.Ivl = min(36500, max(st.Ivl+2, n))
			st.Ease += 150
			st.Due = p.Day + st.Ivl
		}
	}
	st.Reps++
	return st
}

func graduate(st CardState, today, ivl int) CardState {
	st.Learn, st.Step, st.LearnAt = 0, 0, 0
	st.Ivl = max(1, ivl)
	st.Due = today + st.Ivl
	return st
}

// Answer records an answer and reports whether the card comes back this session.
func (p *Progress) Answer(id int64, ans int, now time.Time) (again bool) {
	p.rollover(now)
	k := p.kind(id, now)
	st := p.Next(id, ans, now)
	p.Cards[strconv.FormatInt(id, 10)] = &st
	if k == kindNew {
		p.NewToday++
	} else if k == kindDue {
		p.RevToday++
	}
	p.Studied++
	p.dirty++
	return st.Learn > 0
}

// IntervalLabel describes when a card would come back after an answer.
func (p *Progress) IntervalLabel(id int64, ans int, now time.Time) string {
	st := p.Next(id, ans, now)
	if st.Learn > 0 {
		m := (st.LearnAt - now.Unix() + 30) / 60
		if m < 60 {
			return fmt.Sprintf("%dm", max(1, m))
		}
		return fmt.Sprintf("%dh", m/60)
	}
	return ivlText(st.Ivl)
}

func ivlText(d int) string {
	switch {
	case d < 31:
		return fmt.Sprintf("%dd", d)
	case d < 365:
		return fmt.Sprintf("%.1fmo", float64(d)/30.4)
	}
	return fmt.Sprintf("%.1fy", float64(d)/365)
}

// ---- a study session ----

type Session struct {
	pk    *Package
	prog  *Progress
	queue []int // card indexes still to show (reviews and new, interleaved)
	learn []int // cards in (re)learning this session
	seen  map[int]bool
	notes map[int]bool // notes answered this session (siblings get buried)
	Done  int
	Total int
}

// NewSession builds today's queue for a deck tree.
func NewSession(pk *Package, prog *Progress, idx []int, newLimit, revLimit int, now time.Time) *Session {
	prog.rollover(now)
	s := &Session{pk: pk, prog: prog, seen: map[int]bool{}, notes: map[int]bool{}}
	var news, revs, learns []int
	noteUsed := map[int]bool{}
	for _, i := range idx {
		switch prog.kind(pk.Cards[i].ID, now) {
		case kindLearn:
			learns = append(learns, i)
			noteUsed[pk.Cards[i].Note] = true
		case kindDue:
			revs = append(revs, i)
		case kindNew:
			news = append(news, i)
		}
	}
	// reviews: most overdue first; one card per note
	sort.SliceStable(revs, func(a, b int) bool {
		return prog.State(pk.Cards[revs[a]].ID).Due < prog.State(pk.Cards[revs[b]].ID).Due
	})
	revs = onePerNote(pk, revs, noteUsed)
	if n := max(0, revLimit-prog.RevToday); len(revs) > n {
		revs = revs[:n]
	}
	news = onePerNote(pk, news, noteUsed)
	if n := max(0, newLimit-prog.NewToday); len(news) > n {
		news = news[:n]
	}
	// interleave new cards evenly among the reviews
	if len(news) > 0 && len(revs) > 0 {
		every := float64(len(revs)+len(news)) / float64(len(news))
		next := every / 2
		ri, ni := 0, 0
		for pos := 0; ri < len(revs) || ni < len(news); pos++ {
			if ni < len(news) && (float64(pos) >= next || ri >= len(revs)) {
				s.queue = append(s.queue, news[ni])
				ni++
				next += every
			} else {
				s.queue = append(s.queue, revs[ri])
				ri++
			}
		}
	} else {
		s.queue = append(revs, news...)
	}
	sort.SliceStable(learns, func(a, b int) bool {
		return prog.State(pk.Cards[learns[a]].ID).LearnAt < prog.State(pk.Cards[learns[b]].ID).LearnAt
	})
	s.learn = learns
	s.Total = len(s.queue) + len(s.learn)
	return s
}

func onePerNote(pk *Package, idx []int, used map[int]bool) []int {
	out := idx[:0:0]
	for _, i := range idx {
		n := pk.Cards[i].Note
		if used[n] {
			continue
		}
		used[n] = true
		out = append(out, i)
	}
	return out
}

// Next picks the card to show: a learning card that's due, else the next
// queued card, else the earliest learning card (shown a little early).
// Returns -1 when the session is finished.
func (s *Session) Next(now time.Time) int {
	if len(s.learn) > 0 {
		st := s.prog.State(s.pk.Cards[s.learn[0]].ID)
		if st == nil || st.LearnAt <= now.Unix() || len(s.queue) == 0 {
			return s.learn[0]
		}
	}
	if len(s.queue) > 0 {
		return s.queue[0]
	}
	return -1
}

// Answer records the answer for card ci (which must be the one Next returned).
func (s *Session) Answer(ci, ans int, now time.Time) {
	from := -1
	for i, v := range s.learn {
		if v == ci {
			from = i
			break
		}
	}
	if from >= 0 {
		s.learn = append(s.learn[:from], s.learn[from+1:]...)
	} else if len(s.queue) > 0 && s.queue[0] == ci {
		s.queue = s.queue[1:]
	}
	again := s.prog.Answer(s.pk.Cards[ci].ID, ans, now)
	s.notes[s.pk.Cards[ci].Note] = true
	if again {
		// keep the learning list ordered by due time
		at := s.prog.State(s.pk.Cards[ci].ID).LearnAt
		pos := sort.Search(len(s.learn), func(i int) bool {
			return s.prog.State(s.pk.Cards[s.learn[i]].ID).LearnAt > at
		})
		s.learn = append(s.learn, 0)
		copy(s.learn[pos+1:], s.learn[pos:])
		s.learn[pos] = ci
	} else {
		s.Done++
	}
}

// Remaining reports the cards left by kind (new, learning, review).
func (s *Session) Remaining(now time.Time) (newN, learnN, revN int) {
	for _, i := range s.queue {
		if s.prog.kind(s.pk.Cards[i].ID, now) == kindNew {
			newN++
		} else {
			revN++
		}
	}
	return newN, len(s.learn), revN
}
