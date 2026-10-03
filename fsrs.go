package main

import (
	"hash/fnv"
	"math"
	"strconv"
)

// FSRS-6, the Free Spaced Repetition Scheduler, following the reference
// implementation (open-spaced-repetition/py-fsrs 6.x, also used by Anki).
// Each card has a stability S (days until recall drops to 90%) and a
// difficulty D (1–10); intervals are chosen so the predicted chance of
// recalling the card when it comes back is the desired retention.

var fsrsDefaultParams = []float64{
	0.212, 1.2931, 2.3065, 8.2956, 6.4133, 0.8334, 3.0194, 0.001, 1.8722, 0.1666,
	0.796, 1.4835, 0.0614, 0.2629, 1.6483, 0.6014, 1.8729, 0.5425, 0.0912, 0.0658,
	0.1542,
}

const (
	fsrsMinStability = 0.001
	fsrsMinD         = 1.0
	fsrsMaxD         = 10.0
)

// Scheduler holds the study settings that shape intervals and queues.
type Scheduler struct {
	w         [21]float64
	retention float64 // desired retention, e.g. 0.9
	maxIvl    int     // days
	randomNew bool    // introduce new cards in a random (but fixed) order
	noFuzz    bool    // tests: exact intervals
	decay     float64
	factor    float64
}

var sched = NewScheduler(nil, 0.9, 36500, false)

// NewScheduler builds a scheduler; params may be nil (or the wrong length)
// for the FSRS-6 defaults.
func NewScheduler(params []float64, retention float64, maxIvl int, randomNew bool) *Scheduler {
	s := &Scheduler{retention: min(0.99, max(0.7, retention)), maxIvl: max(1, maxIvl), randomNew: randomNew}
	if len(params) != len(fsrsDefaultParams) {
		params = fsrsDefaultParams
	}
	copy(s.w[:], params)
	s.decay = -s.w[20]
	s.factor = math.Pow(0.9, 1/s.decay) - 1
	return s
}

// applyConfig points the scheduler at the current settings.
func applyConfig(c Config) {
	sched = NewScheduler(c.FSRSParams, c.DesiredRetention, c.MaxInterval, c.NewOrder != "deck")
}

// retrievability is the predicted chance of recall after elapsed days.
func (s *Scheduler) retrievability(elapsed, stability float64) float64 {
	return math.Pow(1+s.factor*max(0, elapsed)/stability, s.decay)
}

// interval is the number of days until recall falls to the desired retention.
func (s *Scheduler) interval(stability float64) int {
	ivl := stability / s.factor * (math.Pow(s.retention, 1/s.decay) - 1)
	return min(s.maxIvl, max(1, int(math.Round(ivl))))
}

func (s *Scheduler) initStability(g int) float64 { return max(fsrsMinStability, s.w[g-1]) }

func (s *Scheduler) initDifficulty(g int) float64 {
	return s.w[4] - math.Exp(s.w[5]*float64(g-1)) + 1
}

func clampD(d float64) float64 { return min(fsrsMaxD, max(fsrsMinD, d)) }

func (s *Scheduler) nextDifficulty(d float64, g int) float64 {
	delta := -s.w[6] * float64(g-3)
	damped := d + (10-d)*delta/9
	return clampD(s.w[7]*s.initDifficulty(4) + (1-s.w[7])*damped)
}

// shortTermStability updates stability for a review on the same day.
func (s *Scheduler) shortTermStability(stab float64, g int) float64 {
	inc := math.Exp(s.w[17]*(float64(g)-3+s.w[18])) * math.Pow(stab, -s.w[19])
	if g >= 2 {
		inc = max(inc, 1)
	}
	return max(fsrsMinStability, stab*inc)
}

// nextStability updates stability for a review on a later day.
func (s *Scheduler) nextStability(d, stab, r float64, g int) float64 {
	var next float64
	if g == answerAgain {
		long := s.w[11] * math.Pow(d, -s.w[12]) * (math.Pow(stab+1, s.w[13]) - 1) * math.Exp((1-r)*s.w[14])
		short := stab / math.Exp(s.w[17]*s.w[18])
		next = min(long, short)
	} else {
		easy := 1.0
		if g == answerEasy {
			easy = s.w[16]
		}
		next = stab * (1 + math.Exp(s.w[8])*(11-d)*math.Pow(stab, -s.w[9])*(math.Exp((1-r)*s.w[10])-1)*easy)
	}
	return max(fsrsMinStability, next)
}

// fromSM2 estimates a card's memory from an SM-2 interval and ease (as Anki
// does when FSRS is switched on for cards without a review history).
func (s *Scheduler) fromSM2(ease, ivl float64) (stab, d float64) {
	const sm2Retention = 0.9
	stab = max(fsrsMinStability, ivl) * s.factor / (math.Pow(sm2Retention, 1/s.decay) - 1)
	d = 11 - (ease-1)/(math.Exp(s.w[8])*math.Pow(stab, -s.w[9])*math.Expm1((1-sm2Retention)*s.w[10]))
	if math.IsNaN(d) || math.IsInf(d, 0) {
		d = 5
	}
	return stab, clampD(d)
}

// fuzz spreads an interval a little, as Anki does, so cards learned together
// don't all come back on the same day. It's derived from the card and its
// review count rather than drawn at random, so the interval shown on a button
// is the one the card gets.
func (s *Scheduler) fuzz(ivl int, id int64, reps int) int {
	if ivl < 3 || s.noFuzz { // py-fsrs: no fuzz below 2.5 days
		return ivl
	}
	delta := 1.0
	for _, r := range [...]struct{ start, end, factor float64 }{{2.5, 7, 0.15}, {7, 20, 0.1}, {20, math.Inf(1), 0.05}} {
		delta += r.factor * max(min(float64(ivl), r.end)-r.start, 0)
	}
	lo := max(2, int(math.Round(float64(ivl)-delta)))
	hi := min(s.maxIvl, int(math.Round(float64(ivl)+delta)))
	lo = min(lo, hi)
	h := fnv.New64a()
	h.Write([]byte(strconv.FormatInt(id, 10) + "/" + strconv.Itoa(reps)))
	u := float64(h.Sum64()>>11) / float64(1<<53)
	return min(hi, lo+int(u*float64(hi-lo+1))) // a whole day in [lo, hi], like Anki
}

// newRank orders new cards: a fixed shuffle of the deck when new cards come
// in random order, the deck's own order otherwise.
func newRank(id int64) uint64 {
	h := fnv.New64a()
	h.Write([]byte("new/" + strconv.FormatInt(id, 10)))
	return h.Sum64()
}
