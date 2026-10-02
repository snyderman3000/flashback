package main

import (
	"fmt"
	"image/color"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type screen int

const (
	scrLibrary screen = iota
	scrDecks
	scrStudy
	scrDone
	scrBrowse
	scrView
	scrBye
)

type LibEntry struct {
	Path   string
	Pkg    *Package
	Prog   *Progress
	Err    string
	Status string // conversion progress while unpacking
	Counts Counts
}

func (e *LibEntry) Name() string {
	if e.Pkg != nil && e.Pkg.Title != "" {
		return e.Pkg.Title
	}
	b := filepath.Base(e.Path)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

type deckRow struct {
	Name   string // full "A::B" path; "" = everything in the file
	Label  string
	Depth  int
	Counts Counts
}

type UI struct {
	env  *Env
	W, H int
	s    float64

	fLogo, fHead, fBody, fSmall, fTiny, fType, fTypeB, fTypeBig *Font

	scr      screen
	lib      []*LibEntry
	libSel   int
	libTop   int
	scanning bool

	cur      *LibEntry
	decks    []deckRow
	deckSel  int
	deckTop  int
	deckName string

	// studying
	sess      *Session
	card      int // index into cur.Pkg.Cards, -1 none
	answer    bool
	layout    *Layout
	cardScrl  int
	flipStart int
	shownAt   time.Time

	// browse and the free-flip viewer
	list     []int
	listSel  int
	listTop  int
	previews map[int][2]string
	viewList []int
	viewPos  int
	viewFrom screen
	shuffled bool

	modalTitle string
	modalBody  []string
	modalCol   color.RGBA

	toast      string
	toastUntil time.Time

	frame    int
	byeStart int
	quit     bool
	post     chan func()
}

func NewUI(env *Env, w, h int) *UI {
	s := float64(h) / 480
	if sw := float64(w) / 640; sw < s {
		s = sw
	}
	u := &UI{env: env, W: w, H: h, s: s, post: make(chan func(), 128), card: -1, previews: map[int][2]string{}}
	u.fLogo = loadFont(titleTTF, 22*s)
	u.fHead = loadFont(bodyTTF, 18*s)
	u.fBody = loadFont(bodyTTF, 16*s)
	u.fSmall = loadFont(bodyTTF, 14*s)
	u.fTiny = loadFont(bodyTTF, 12*s)
	u.fType = loadFont(cpRegular, 18*s)
	u.fTypeB = loadFont(cpBold, 19*s)
	u.fTypeBig = loadFont(cpBold, 26*s)
	return u
}

func (u *UI) px(v int) int { return int(math.Round(float64(v) * u.s)) }

func (u *UI) now() time.Time { return u.env.Now() }

// ---------- lifecycle ----------

func (u *UI) Start() {
	if p := u.env.Config.Problem; p != "" {
		u.message("CONFIG.JSON", ruleRed, p)
	}
	u.scan()
}

// scan finds deck files and unpacks/loads them one at a time in the background.
func (u *UI) scan() {
	paths := u.env.FindPackages()
	old := map[string]*LibEntry{}
	for _, e := range u.lib {
		old[e.Path] = e
	}
	var lib []*LibEntry
	var todo []*LibEntry
	for _, p := range paths {
		if e := old[p]; e != nil && e.Pkg != nil {
			if k, err := packageKey(p); err == nil && k == e.Pkg.Key {
				lib = append(lib, e)
				continue
			}
		}
		e := &LibEntry{Path: p, Status: "WAITING"}
		lib = append(lib, e)
		todo = append(todo, e)
	}
	for _, e := range old {
		found := false
		for _, n := range lib {
			if n == e {
				found = true
			}
		}
		if !found && e.Pkg != nil {
			e.Pkg.Close()
		}
	}
	u.lib = lib
	u.libSel = min(u.libSel, max(0, len(lib)-1))
	if len(todo) == 0 {
		u.refreshCounts()
		return
	}
	u.scanning = true
	cacheRoot := filepath.Join(u.env.DataDir, "cache")
	go func() {
		u.env.PruneCache(paths)
		for _, e := range todo {
			e := e
			pk, err := OpenPackage(e.Path, cacheRoot, func(s string) {
				u.post <- func() { e.Status = s }
			})
			var prog *Progress
			if err == nil {
				prog = LoadProgress(u.env.DataDir, e.Path, u.now())
			} else {
				log.Printf("%s: %v", e.Path, err)
			}
			u.post <- func() {
				e.Pkg, e.Prog, e.Status = pk, prog, ""
				if err != nil {
					e.Err = err.Error()
				} else {
					e.Counts = u.countFor(e, "")
				}
			}
		}
		u.post <- func() { u.scanning = false }
	}()
}

func (u *UI) refreshCounts() {
	for _, e := range u.lib {
		if e.Pkg != nil {
			e.Prog.rollover(u.now())
			e.Counts = u.countFor(e, "")
		}
	}
}

// countFor returns counts for a deck tree with today's limits applied to new/due.
func (u *UI) countFor(e *LibEntry, deck string) Counts {
	c := e.Prog.Count(e.Pkg, e.Pkg.CardsIn(deck), u.now())
	c.New = min(c.New, max(0, u.env.Config.NewPerDay-e.Prog.NewToday))
	c.Due = min(c.Due, max(0, u.env.Config.ReviewsPerDay-e.Prog.RevToday))
	return c
}

func (u *UI) saveAll() {
	for _, e := range u.lib {
		if e.Prog != nil && e.Prog.dirty > 0 {
			if err := e.Prog.Save(); err != nil {
				log.Printf("saving progress: %v", err)
			}
		}
	}
	u.env.SaveConfig()
}

func (u *UI) showToast(s string) {
	u.toast = s
	u.toastUntil = time.Now().Add(2500 * time.Millisecond)
}

func (u *UI) message(title string, col color.RGBA, body string) {
	u.modalTitle, u.modalCol = title, col
	u.modalBody = strings.Split(body, "\n")
}

func (u *UI) Tick() {
	if u.toast != "" && time.Now().After(u.toastUntil) {
		u.toast = ""
	}
}

func (u *UI) Animating() bool {
	return u.scanning || u.flipping() || u.toast != "" || u.scr == scrBye
}

const flipFrames = 6

func (u *UI) flipping() bool { return u.flipStart > 0 && u.frame-u.flipStart < flipFrames }

// ---------- opening things ----------

func (u *UI) openLib(e *LibEntry) {
	if e.Pkg == nil {
		if e.Err != "" {
			u.message("CAN'T READ THIS FILE", ruleRed, filepath.Base(e.Path)+"\n\n"+e.Err+"\n\nExport it again from Anki (File → Export → Anki Deck Package) and copy it over.")
		} else {
			u.showToast("STILL UNPACKING — ONE MOMENT")
		}
		return
	}
	u.cur = e
	e.Prog.rollover(u.now())
	u.buildDecks()
	u.deckSel, u.deckTop = 0, 0
	u.scr = scrDecks
}

func (u *UI) buildDecks() {
	e := u.cur
	u.decks = u.decks[:0]
	tops := map[string]bool{}
	for _, d := range e.Pkg.Decks {
		tops[strings.SplitN(d.Name, "::", 2)[0]] = true
	}
	if len(tops) > 1 || len(e.Pkg.Decks) == 0 {
		u.decks = append(u.decks, deckRow{Name: "", Label: "ALL DECKS IN THIS FILE", Depth: -1, Counts: u.countFor(e, "")})
	}
	for _, d := range e.Pkg.Decks {
		parts := strings.Split(d.Name, "::")
		u.decks = append(u.decks, deckRow{Name: d.Name, Label: parts[len(parts)-1], Depth: len(parts) - 1, Counts: u.countFor(e, d.Name)})
	}
	if u.deckSel >= len(u.decks) {
		u.deckSel = 0
	}
}

func (u *UI) selDeck() deckRow {
	if u.deckSel < len(u.decks) {
		return u.decks[u.deckSel]
	}
	return deckRow{}
}

func (u *UI) startStudy() {
	d := u.selDeck()
	u.deckName = d.Name
	u.sess = NewSession(u.cur.Pkg, u.cur.Prog, u.cur.Pkg.CardsIn(d.Name), u.env.Config.NewPerDay, u.env.Config.ReviewsPerDay, u.now())
	u.nextStudyCard()
}

func (u *UI) nextStudyCard() {
	ci := u.sess.Next(u.now())
	if ci < 0 {
		u.card = -1
		u.layout = nil
		u.scr = scrDone
		u.cur.Prog.Save()
		return
	}
	u.scr = scrStudy
	u.showCard(ci, false)
}

func (u *UI) showCard(ci int, answer bool) {
	u.card, u.answer = ci, answer
	u.cardScrl = 0
	u.layout = u.layoutCard(ci, answer)
	u.shownAt = time.Now()
	if answer && u.layout.AnswerY > 0 {
		// jump to the answer when the question was long
		if u.layout.AnswerY > u.cardTextH()/2 {
			u.cardScrl = min(u.maxScroll(), u.layout.AnswerY-u.px(10))
		}
	}
}

func (u *UI) layoutCard(ci int, answer bool) *Layout {
	w, _ := u.cardTextArea()
	h := u.cardTextH()
	html := u.cur.Pkg.CardHTML(ci, answer)
	return LayoutHTML(html, w, u.textSize(), h*3/5, u.lineGap(), u.gridBase(), u.cur.Pkg)
}

func (u *UI) textSize() int { return int(math.Round(float64(u.env.Config.TextSize) * u.s)) }

func (u *UI) maxScroll() int {
	if u.layout == nil {
		return 0
	}
	return max(0, u.layout.H-u.cardTextH())
}

func (u *UI) flip() {
	if u.card < 0 {
		return
	}
	u.flipStart = u.frame
	u.showCard(u.card, !u.answer)
}

func (u *UI) answerCard(ans int) {
	u.sess.Answer(u.card, ans, u.now())
	if u.cur.Prog.dirty >= 10 {
		u.cur.Prog.Save()
	}
	u.nextStudyCard()
}

func (u *UI) leaveStudy() {
	if u.cur != nil && u.cur.Prog != nil {
		if err := u.cur.Prog.Save(); err != nil {
			u.showToast("COULDN'T SAVE PROGRESS")
		}
	}
	u.sess = nil
	u.buildDecks()
	u.scr = scrDecks
}

func (u *UI) startBrowse() {
	d := u.selDeck()
	u.deckName = d.Name
	u.list = u.cur.Pkg.CardsIn(d.Name)
	u.listSel, u.listTop = 0, 0
	u.previews = map[int][2]string{}
	u.scr = scrBrowse
}

var answerHR = regexp.MustCompile(`(?i)<hr[^>]*\bid\s*=\s*["']?answer\b[^>]*>`)

func (u *UI) preview(ci int) [2]string {
	if p, ok := u.previews[ci]; ok {
		return p
	}
	q := previewText(u.cur.Pkg.CardHTML(ci, false))
	ah := u.cur.Pkg.CardHTML(ci, true)
	// like Anki's browser: the answer is what follows the <hr id=answer> divider
	if loc := answerHR.FindStringIndex(ah); loc != nil {
		ah = ah[loc[1]:]
	}
	a := previewText(ah)
	a = strings.TrimSpace(strings.TrimPrefix(a, q))
	if q == "" {
		q = "(blank)"
	}
	p := [2]string{q, a}
	u.previews[ci] = p
	return p
}

func (u *UI) startShuffle() {
	d := u.selDeck()
	u.deckName = d.Name
	u.viewList = append([]int(nil), u.cur.Pkg.CardsIn(d.Name)...)
	rand.Shuffle(len(u.viewList), func(i, j int) { u.viewList[i], u.viewList[j] = u.viewList[j], u.viewList[i] })
	u.shuffled = true
	u.viewFrom = scrDecks
	u.viewPos = 0
	u.scr = scrView
	if len(u.viewList) > 0 {
		u.showCard(u.viewList[0], false)
	}
}

func (u *UI) viewStep(d int) {
	if len(u.viewList) == 0 {
		return
	}
	np := u.viewPos + d
	if np < 0 || np >= len(u.viewList) {
		if u.shuffled && np >= len(u.viewList) {
			u.showToast("END OF THE PILE — Y TO RESHUFFLE")
		}
		return
	}
	u.viewPos = np
	u.showCard(u.viewList[np], false)
}

func (u *UI) cycleTextSize() {
	sizes := []int{18, 22, 26, 30}
	cur := u.env.Config.TextSize
	next := sizes[0]
	for _, s := range sizes {
		if s > cur {
			next = s
			break
		}
	}
	u.env.Config.TextSize = next
	if u.card >= 0 && u.cur != nil {
		u.showCard(u.card, u.answer)
	}
	labels := map[int]string{18: "SMALL", 22: "MEDIUM", 26: "LARGE", 30: "HUGE"}
	u.showToast("TEXT SIZE: " + labels[next])
}

// ---------- input ----------

func (u *UI) Key(k int) {
	if k == BtnMenu {
		u.quit = true
		return
	}
	if u.modalTitle != "" {
		if k == BtnA || k == BtnB || k == BtnStart {
			u.modalTitle = ""
		}
		return
	}
	switch u.scr {
	case scrLibrary:
		u.keyLibrary(k)
	case scrDecks:
		u.keyDecks(k)
	case scrStudy:
		u.keyStudy(k)
	case scrDone:
		switch k {
		case BtnA, BtnY:
			u.startShuffle()
		case BtnB:
			u.leaveStudy()
		}
	case scrBrowse:
		u.keyBrowse(k)
	case scrView:
		u.keyView(k)
	}
}

func (u *UI) keyLibrary(k int) {
	n := len(u.lib)
	per := u.libRowsPerPage()
	switch k {
	case BtnUp:
		if n > 0 {
			u.libSel = (u.libSel - 1 + n) % n
		}
	case BtnDown:
		if n > 0 {
			u.libSel = (u.libSel + 1) % n
		}
	case BtnL1, BtnLeft:
		u.libSel = max(0, u.libSel-per)
	case BtnR1, BtnRight:
		u.libSel = max(0, min(n-1, u.libSel+per))
	case BtnA:
		if u.libSel < n {
			u.openLib(u.lib[u.libSel])
		}
	case BtnSelect:
		u.scan()
		u.showToast("LOOKING FOR NEW DECKS")
	case BtnB:
		u.quit = true
	}
}

func (u *UI) keyDecks(k int) {
	n := len(u.decks)
	switch k {
	case BtnUp:
		u.deckSel = (u.deckSel - 1 + n) % n
	case BtnDown:
		u.deckSel = (u.deckSel + 1) % n
	case BtnL1, BtnLeft:
		u.deckSel = max(0, u.deckSel-u.deckRowsPerPage())
	case BtnR1, BtnRight:
		u.deckSel = min(n-1, u.deckSel+u.deckRowsPerPage())
	case BtnA:
		u.startStudy()
	case BtnX:
		u.startBrowse()
	case BtnY:
		u.startShuffle()
	case BtnB:
		u.cur.Prog.Save()
		u.refreshCounts()
		u.scr = scrLibrary
	}
}

func (u *UI) scrollCard(d int) {
	u.cardScrl = max(0, min(u.maxScroll(), u.cardScrl+d))
}

func (u *UI) keyStudy(k int) {
	step := u.cardTextH() / 3
	switch k {
	case BtnUp:
		u.scrollCard(-step)
		return
	case BtnDown:
		u.scrollCard(step)
		return
	case BtnSelect:
		u.cycleTextSize()
		return
	case BtnB:
		u.leaveStudy()
		return
	}
	if u.flipping() {
		return
	}
	if !u.answer {
		if k == BtnA || k == BtnX || k == BtnY || k == BtnRight {
			u.flip()
		}
		return
	}
	switch k {
	case BtnY:
		u.answerCard(answerAgain)
	case BtnA:
		u.answerCard(answerGood)
	case BtnX:
		u.answerCard(answerEasy)
	}
}

func (u *UI) keyBrowse(k int) {
	n := len(u.list)
	per := u.listRowsPerPage()
	switch k {
	case BtnUp:
		if n > 0 {
			u.listSel = (u.listSel - 1 + n) % n
		}
	case BtnDown:
		if n > 0 {
			u.listSel = (u.listSel + 1) % n
		}
	case BtnL1, BtnLeft:
		u.listSel = max(0, u.listSel-per)
	case BtnR1, BtnRight:
		u.listSel = max(0, min(n-1, u.listSel+per))
	case BtnA:
		if n > 0 {
			u.viewList = u.list
			u.viewPos = u.listSel
			u.shuffled = false
			u.viewFrom = scrBrowse
			u.scr = scrView
			u.showCard(u.list[u.listSel], false)
		}
	case BtnB:
		u.scr = scrDecks
	}
}

func (u *UI) keyView(k int) {
	step := u.cardTextH() / 3
	switch k {
	case BtnUp:
		u.scrollCard(-step)
	case BtnDown:
		u.scrollCard(step)
	case BtnLeft, BtnL1:
		u.viewStep(-1)
	case BtnRight, BtnR1:
		u.viewStep(1)
	case BtnA:
		if u.flipping() {
			return
		}
		if u.answer && u.shuffled {
			u.viewStep(1)
			return
		}
		u.flip()
	case BtnX:
		if !u.flipping() {
			u.flip()
		}
	case BtnY:
		if u.shuffled {
			rand.Shuffle(len(u.viewList), func(i, j int) { u.viewList[i], u.viewList[j] = u.viewList[j], u.viewList[i] })
			u.viewPos = 0
			u.showCard(u.viewList[0], false)
			u.showToast("SHUFFLED")
		}
	case BtnSelect:
		u.cycleTextSize()
	case BtnB:
		if u.viewFrom == scrBrowse {
			u.listSel = u.viewPos
			u.scr = scrBrowse
		} else {
			u.buildDecks()
			u.scr = scrDecks
		}
	}
}

// ---------- device status ----------

func battery() int {
	b, err := os.ReadFile("/tmp/percBat")
	if err != nil {
		return -1
	}
	var v int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &v); err != nil || v < 0 || v > 100 {
		return -1
	}
	return v
}
