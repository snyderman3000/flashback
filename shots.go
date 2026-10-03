package main

// Demo scenes for screenshots (-shot) and the app icon (-icon).

import (
	"image"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func demoDir() string {
	if d := os.Getenv("FLASHBACK_DEMO"); d != "" {
		return d
	}
	return "testdata"
}

func demoLoad(u *UI) {
	cache, _ := os.MkdirTemp("", "flashback-demo")
	names := map[string]string{
		"new.apkg":     "Spanish & Science (sampler).apkg",
		"genanki.apkg": "Country Capitals.apkg",
		"ug_new.apkg":  "Ultimate Geography [EN].apkg", // CC BY 3.0, github.com/anki-geo/ultimate-geography
	}
	for src, nice := range names {
		p := filepath.Join(demoDir(), src)
		pk, err := OpenPackage(p, cache, nil)
		if err != nil {
			continue
		}
		pk.Title = strings.TrimSuffix(nice, ".apkg")
		e := &LibEntry{Path: filepath.Join("/mnt/SDCARD/Anki", nice), Pkg: pk}
		e.Prog = &Progress{Cards: map[string]*CardState{}}
		e.Prog.rollover(u.now())
		u.lib = append(u.lib, e)
	}
	// sort like the real scan does (by file name)
	sort.Slice(u.lib, func(i, j int) bool {
		return strings.ToLower(filepath.Base(u.lib[i].Path)) < strings.ToLower(filepath.Base(u.lib[j].Path))
	})
	if e := demoEntry(u, "Ultimate"); e != nil {
		for i, c := range e.Pkg.Cards {
			if i%4 == 1 {
				e.Prog.Cards[itoa(c.ID)] = &CardState{Ivl: 9, Ease: 2500, Due: e.Prog.Day, Reps: 4}
			}
		}
	}
	// a little history so the counts aren't all zero
	if e := demoEntry(u, "Spanish"); e != nil {
		now := u.now()
		for i, c := range e.Pkg.Cards {
			if i%3 == 0 {
				e.Prog.Cards[itoa(c.ID)] = &CardState{Ivl: 3, Ease: 2500, Due: e.Prog.Day - i%2, Reps: 2}
			}
			if i == 4 {
				e.Prog.Cards[itoa(c.ID)] = &CardState{Ease: 2500, Learn: 1, Step: 1, LearnAt: now.Unix() - 30}
			}
		}
	}
	u.refreshCounts()
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func demoEntry(u *UI, prefix string) *LibEntry {
	for _, e := range u.lib {
		if strings.HasPrefix(e.Name(), prefix) {
			return e
		}
	}
	return nil
}

func demoCard(u *UI, first string, ord int) int {
	for i, c := range u.cur.Pkg.Cards {
		if c.Ord == ord && strings.HasPrefix(u.cur.Pkg.Fields(c.Note)[0], first) {
			return i
		}
	}
	return 0
}

func demoScene(u *UI, scene string) {
	if scene == "empty" {
		u.scr = scrLibrary
		return
	}
	demoLoad(u)
	u.scr = scrLibrary
	switch scene {
	case "library":
		u.libSel = 1
	case "settings":
		u.env.Config.NewPerDay, u.env.Config.ReviewsPerDay, u.env.Config.NewOrder = 80, 800, "random"
		u.openSettings()
		u.setSel = 2
	case "unpack":
		u.lib = append(u.lib, &LibEntry{Path: "/mnt/SDCARD/Anki/Ultimate Geography.apkg", Status: "READING NOTES 2000"})
		u.libSel = 2
		u.frame = 7
	case "decks", "study", "answer", "cloze", "occlusion", "browse", "view", "done", "japanese":
		u.openLib(demoEntry(u, "Spanish"))
		u.deckSel = 2 // Spanish Basics
		switch scene {
		case "study", "answer":
			u.startStudy()
			u.showCard(demoCard(u, "perro", 0), scene == "answer")
		case "cloze":
			u.deckSel = 1
			u.deckName = "Science"
			u.sess = NewSession(u.cur.Pkg, u.cur.Prog, u.cur.Pkg.CardsIn("Science"), 20, 200, u.now())
			u.scr = scrStudy
			u.showCard(demoCard(u, "The {{c1::mito", 1), true)
		case "occlusion":
			u.deckSel = 1
			u.deckName = "Science"
			u.sess = NewSession(u.cur.Pkg, u.cur.Prog, u.cur.Pkg.CardsIn("Science"), 20, 200, u.now())
			u.scr = scrStudy
			u.showCard(demoCard(u, "{{c1::image-occ", 1), false)
		case "browse":
			u.deckSel = 1
			u.startBrowse()
			u.listSel = 3
		case "view":
			u.startBrowse()
			u.keyBrowse(BtnDown)
			u.keyBrowse(BtnDown)
			u.keyBrowse(BtnDown)
			u.keyBrowse(BtnA)
			u.showCard(u.viewList[u.viewPos], true)
		case "japanese":
			u.deckSel = 4
			u.startShuffle()
			u.viewList = u.cur.Pkg.CardsIn("日本語")
			u.viewPos = 1
			u.showCard(u.viewList[1], true)
		case "done":
			u.deckName = "Spanish Basics"
			u.sess = &Session{Done: 7}
			u.scr = scrDone
		}
	case "flag", "flag-answer", "map", "geo-decks", "geo-browse":
		u.openLib(demoEntry(u, "Ultimate"))
		u.deckSel = 0
		switch scene {
		case "flag", "flag-answer":
			u.startStudy()
			u.showCard(demoCard(u, "Brazil", 2), scene == "flag-answer")
		case "map":
			u.startStudy()
			u.showCard(demoCard(u, "Japan", 3), true)
		case "geo-browse":
			u.startBrowse()
			u.listSel = 9
		}
	}
}

// appIcon: an index card with a red header line, lines, and a typed "A",
// with a second card peeking out behind it.
func appIcon(n int) *image.RGBA {
	c := NewCanvas(n, n)
	for i := range c.Pix {
		c.Pix[i] = 0
	}
	s := float64(n) / 74
	p := func(v float64) int { return int(v*s + 0.5) }
	// back card
	c.Fill(p(14), p(10), p(56), p(40), paperDim)
	c.Fill(p(14), p(17), p(56), 1, mix(paperDim, ruleRed, 0.6))
	// front card with shadow
	c.Blend(p(7), p(25), p(58), p(42), colBlack, 0.55)
	c.Fill(p(4), p(22), p(58), p(42), paperCol)
	c.Fill(p(4), p(30), p(58), max(1, p(1.6)), ruleRed)
	for y := 40.0; y < 64; y += 7 {
		c.Fill(p(4), p(y), p(58), 1, ruleBlue)
	}
	// typed letters
	f := cardFont(max(8, p(18)), 1)
	c.TextSet(f, p(10), p(32), "Aa", inkCol)
	// amber flash
	bolt := [][2]float64{{54, 2}, {40, 26}, {50, 26}, {42, 46}, {64, 18}, {53, 18}, {62, 2}}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			fx, fy := (float64(x)+0.5)/s, (float64(y)+0.5)/s
			in := false
			for i, j := 0, len(bolt)-1; i < len(bolt); j, i = i, i+1 {
				xi, yi, xj, yj := bolt[i][0], bolt[i][1], bolt[j][0], bolt[j][1]
				if (yi > fy) != (yj > fy) && fx < (xj-xi)*(fy-yi)/(yj-yi)+xi {
					in = !in
				}
			}
			if in {
				c.Set(x, y, colAmber)
			}
		}
	}
	return c.RGBA
}
