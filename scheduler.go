package main

// Spaced repetition with FSRS (fsrs.go), Anki-style.
//
// New cards go through two short learning steps (1 min, 10 min) and then
// graduate to an interval FSRS picks from the card's stability (Easy skips the
// steps). Review cards come back when their predicted recall falls to the
// desired retention; Again sends them through a 10-minute relearning step.
// Days roll over at 4 a.m. local time. Cards studied with an earlier
// Flashback (SM-2: interval and ease) are converted the first time they're
// answered again.
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
	Ivl     int     `json:"i,omitempty"`  // days
	Ease    int     `json:"e,omitempty"`  // SM-2 ease in permille (older progress only)
	S       float64 `json:"s2,omitempty"` // FSRS stability, days
	D       float64 `json:"df,omitempty"` // FSRS difficulty, 1–10
	LastDay int     `json:"lr,omitempty"` // day number of the last answer
	Due     int     `json:"d,omitempty"`  // day number, for review cards
	Reps    int     `json:"r,omitempty"`
	Lapses  int     `json:"l,omitempty"`
	Learn   int     `json:"ln,omitempty"` // 0 review, 1 learning, 2 relearning
	Step    int     `json:"s,omitempty"`
	LearnAt int64   `json:"t,omitempty"` // unix seconds when a learning card is due
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
		st = CardState{Learn: 1}
	}
	today := dayNumber(now)
	t := now.Unix()
	st = migrateSM2(st)

	// update the card's memory: stability and difficulty
	elapsed := today - st.LastDay
	switch {
	case st.S == 0: // first answer
		st.S, st.D = sched.initStability(ans), clampD(sched.initDifficulty(ans))
	case elapsed < 1: // again on the same day: learning steps
		st.S, st.D = sched.shortTermStability(st.S, ans), sched.nextDifficulty(st.D, ans)
	default:
		r := sched.retrievability(float64(elapsed), st.S)
		st.S, st.D = sched.nextStability(st.D, st.S, r, ans), sched.nextDifficulty(st.D, ans)
	}

	// then when it comes back
	switch st.Learn {
	case 1: // new or learning
		switch ans {
		case answerAgain:
			st.Step = 0
			st.LearnAt = t + learnSteps[0]
		case answerGood:
			st.Step++
			if st.Step >= len(learnSteps) {
				st = graduate(st, today, id)
			} else {
				st.LearnAt = t + learnSteps[st.Step]
			}
		default:
			st = graduate(st, today, id)
		}
	case 2: // relearning after a lapse
		if ans == answerAgain {
			st.LearnAt = t + relearnStep
		} else {
			st = graduate(st, today, id)
		}
	default: // review
		if ans == answerAgain {
			st.Lapses++
			st.Learn, st.Step, st.LearnAt = 2, 0, t+relearnStep
		} else {
			st.Ivl = sched.fuzz(sched.interval(st.S), id, st.Reps)
			st.Due = today + st.Ivl
		}
	}
	st.LastDay = today
	st.Reps++
	return st
}

// graduate moves a (re)learning card to review, due when FSRS says.
func graduate(st CardState, today int, id int64) CardState {
	st.Learn, st.Step, st.LearnAt = 0, 0, 0
	st.Ivl = sched.fuzz(sched.interval(st.S), id, st.Reps)
	st.Due = today + st.Ivl
	return st
}

// migrateSM2 gives a card studied with SM-2 an FSRS memory estimated from its
// interval and ease. Cards still in their first learning steps simply start
// over with their next answer.
func migrateSM2(st CardState) CardState {
	if st.S > 0 || st.Ivl <= 0 || st.Learn == 1 {
		return st
	}
	ease := float64(st.Ease) / 1000
	if ease <= 1 {
		ease = 2.5
	}
	st.S, st.D = sched.fromSM2(ease, float64(st.Ivl))
	if st.Learn == 2 {
		st.LastDay = dayNumber(time.Unix(st.LearnAt-relearnStep, 0))
	} else {
		st.LastDay = st.Due - st.Ivl
	}
	return st
}

// recall is a card's predicted chance of being remembered today.
func (p *Progress) recall(id int64, today int) float64 {
	st := p.State(id)
	if st == nil {
		return 0
	}
	m := migrateSM2(*st)
	if m.S == 0 {
		return 0
	}
	return sched.retrievability(float64(today-m.LastDay), m.S)
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
	// reviews: the ones most likely forgotten first (FSRS's "retrievability
	// ascending"), so a review limit cuts the safest cards; one card per note
	today := dayNumber(now)
	rec := make(map[int]float64, len(revs))
	for _, i := range revs {
		rec[i] = prog.recall(pk.Cards[i].ID, today)
	}
	sort.SliceStable(revs, func(a, b int) bool { return rec[revs[a]] < rec[revs[b]] })
	revs = onePerNote(pk, revs, noteUsed)
	if n := max(0, revLimit-prog.RevToday); len(revs) > n {
		revs = revs[:n]
	}
	if sched.randomNew {
		sort.SliceStable(news, func(a, b int) bool { return newRank(pk.Cards[news[a]].ID) < newRank(pk.Cards[news[b]].ID) })
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

// Upcoming guesses the card Next will return once ci (the card Next just
// returned) has been answered Good, so it can be prepared ahead of time. It
// changes nothing; a wrong guess only costs some background work.
func (s *Session) Upcoming(ci int, now time.Time) int {
	learn, queue := s.learn, s.queue
	if len(learn) > 0 && learn[0] == ci {
		learn = learn[1:]
	} else if len(queue) > 0 && queue[0] == ci {
		queue = queue[1:]
	}
	if len(learn) > 0 {
		st := s.prog.State(s.pk.Cards[learn[0]].ID)
		if st == nil || st.LearnAt <= now.Unix() || len(queue) == 0 {
			return learn[0]
		}
	}
	if len(queue) > 0 {
		return queue[0]
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
