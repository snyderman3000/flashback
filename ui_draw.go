package main

import (
	"fmt"
	"image/color"
	"math"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	colNew   = rgb(0x1f, 0x56, 0xc8)
	colLearn = rgb(0xc0, 0x39, 0x2b)
	colDue   = rgb(0x2e, 0x7d, 0x32)
	colDesk  = rgb(0x14, 0x12, 0x10)
	colDesk2 = rgb(0x1c, 0x19, 0x15)
	colMark  = rgb(0xff, 0xe0, 0x6a) // highlighter
	paperDim = rgb(0xc9, 0xc1, 0xab)
)

func (u *UI) topH() int       { return u.px(44) }
func (u *UI) keysH() int      { return u.px(36) }
func (u *UI) contentTop() int { return u.topH() }
func (u *UI) contentBot() int { return u.H - u.keysH() }

// ---------- frame ----------

func (u *UI) Draw(c *Canvas) {
	u.drawDesk(c)
	switch u.scr {
	case scrLibrary:
		u.drawLibrary(c)
	case scrDecks:
		u.drawDecks(c)
	case scrStudy, scrView:
		u.drawCardScreen(c)
	case scrDone:
		u.drawDone(c)
	case scrBrowse:
		u.drawBrowse(c)
	case scrSettings:
		u.drawSettings(c)
	}
	u.drawTop(c)
	u.drawKeys(c)
	if u.toast != "" {
		u.drawToast(c)
	}
	if u.modalTitle != "" {
		u.drawModal(c)
	}
}

func (u *UI) drawDesk(c *Canvas) {
	c.Fill(0, 0, u.W, u.H, colDesk)
	// faint wood grain
	for y := 0; y < u.H; y += u.px(6) {
		v := math.Sin(float64(y)*0.07) * math.Sin(float64(y)*0.013+1)
		if v > 0.35 {
			c.Fill(0, y, u.W, u.px(2), colDesk2)
		}
	}
}

func (u *UI) drawTop(c *Canvas) {
	h := u.topH()
	c.Fill(0, 0, u.W, h, colPanel)
	c.Fill(0, h-u.px(2), u.W, u.px(2), colAmberDk)
	c.Fill(0, h-u.px(2), u.W/3, u.px(2), colAmber)
	// logo: two little cards and the word
	x := u.px(14)
	iy := (h - u.px(22)) / 2
	c.Fill(x+u.px(5), iy, u.px(24), u.px(17), paperDim)
	c.Fill(x, iy+u.px(5), u.px(24), u.px(17), paperCol)
	c.Fill(x, iy+u.px(9), u.px(24), 1, ruleRed)
	c.Fill(x+u.px(3), iy+u.px(14), u.px(18), 1, ruleBlue)
	c.Fill(x+u.px(3), iy+u.px(18), u.px(18), 1, ruleBlue)
	x += u.px(34)
	x = c.TextGlow(u.fLogo, x, (h-u.fLogo.height)/2-u.px(1), "FLASHBACK", colAmber)
	// breadcrumb
	crumb := ""
	switch u.scr {
	case scrLibrary:
		crumb = "CARD CATALOG"
	case scrSettings:
		crumb = "SETTINGS"
	case scrDecks:
		crumb = u.cur.Name()
	case scrStudy, scrDone:
		crumb = "STUDY · " + u.deckLabel()
	case scrBrowse:
		crumb = "BROWSE · " + u.deckLabel()
	case scrView:
		if u.shuffled {
			crumb = "SHUFFLE · " + u.deckLabel()
		} else {
			crumb = "BROWSE · " + u.deckLabel()
		}
	}
	right := u.W - u.px(14)
	// battery + clock
	if b := battery(); b >= 0 {
		bw, bh := u.px(26), u.px(12)
		by := (h - bh) / 2
		c.Border(right-bw, by, bw, bh, 1, colPaperDim)
		c.Fill(right, by+bh/4, u.px(2), bh/2, colPaperDim)
		fill := (bw - 4) * b / 100
		col := colGreen
		if b <= 20 {
			col = colRed
		}
		c.Fill(right-bw+2, by+2, fill, bh-4, col)
		right -= bw + u.px(10)
	}
	clock := time.Now().Format("3:04")
	right -= u.fHead.Width(clock)
	c.Text(u.fHead, right, (h-u.fHead.height)/2, clock, colPaperDim)
	right -= u.px(16)
	if crumb != "" {
		x += u.px(14)
		c.Fill(x, h/2-u.px(8), 1, u.px(16), colGrid)
		x += u.px(12)
		fs := cardFont(u.px(15), 0)
		crumb = fs.Ellipsize(strings.ToUpper(crumb), right-x)
		c.TextSet(fs, x, (h-fs.height)/2, crumb, colAmberDim)
	}
}

func (u *UI) deckLabel() string {
	if u.deckName == "" {
		return u.cur.Name()
	}
	parts := strings.Split(u.deckName, "::")
	return parts[len(parts)-1]
}

func (u *UI) drawKeys(c *Canvas) {
	var items [][2]string
	switch u.scr {
	case scrLibrary:
		if len(u.lib) > 0 {
			items = [][2]string{{"A", "OPEN"}, {"START", "SETTINGS"}, {"SELECT", "RESCAN"}, {"B", "EXIT"}}
		} else {
			items = [][2]string{{"START", "SETTINGS"}, {"SELECT", "RESCAN"}, {"B", "EXIT"}}
		}
	case scrDecks:
		items = [][2]string{{"A", "STUDY"}, {"X", "BROWSE"}, {"Y", "SHUFFLE"}, {"START", "SETTINGS"}, {"B", "BACK"}}
	case scrSettings:
		items = [][2]string{{"◀▶", "CHANGE"}, {"L1/R1", "BIG STEPS"}, {"B", "SAVE & BACK"}}
	case scrStudy:
		if u.answer && !u.flipping() {
			id := u.cur.Pkg.Cards[u.card].ID
			now := u.now()
			items = [][2]string{
				{"Y", "AGAIN " + u.cur.Prog.IntervalLabel(id, answerAgain, now)},
				{"A", "GOOD " + u.cur.Prog.IntervalLabel(id, answerGood, now)},
				{"X", "EASY " + u.cur.Prog.IntervalLabel(id, answerEasy, now)},
			}
		} else {
			items = [][2]string{{"A", "SHOW ANSWER"}, {"SELECT", "TEXT SIZE"}, {"B", "BACK"}}
		}
	case scrDone:
		items = [][2]string{{"A", "SHUFFLE THIS DECK"}, {"B", "BACK"}}
	case scrBrowse:
		items = [][2]string{{"A", "OPEN"}, {"L1/R1", "PAGE"}, {"B", "BACK"}}
	case scrView:
		if u.shuffled {
			if u.answer {
				items = [][2]string{{"A", "NEXT"}, {"◀▶", "PREV/NEXT"}, {"Y", "RESHUFFLE"}, {"B", "BACK"}}
			} else {
				items = [][2]string{{"A", "FLIP"}, {"◀▶", "PREV/NEXT"}, {"Y", "RESHUFFLE"}, {"B", "BACK"}}
			}
		} else {
			items = [][2]string{{"A", "FLIP"}, {"◀▶", "PREV/NEXT"}, {"B", "BACK"}}
		}
	}
	y := u.H - u.keysH()
	c.Fill(0, y, u.W, u.keysH(), colPanel)
	c.Fill(0, y, u.W, 1, colGrid)
	x := u.px(14)
	by := y + (u.keysH()-u.fSmall.height-2)/2
	for _, it := range items {
		col := colAmber
		switch it[0] {
		case "Y":
			if u.scr == scrStudy {
				col = colLearn
			}
		case "X":
			col = colCyan
		case "B", "SELECT", "L1/R1", "◀▶", "START":
			col = colPaperDim
		}
		x = c.Button(u.fSmall, x, by, it[0], it[1], col)
	}
}

func (u *UI) drawToast(c *Canvas) {
	w := u.fBody.Width(u.toast) + u.px(28)
	h := u.fBody.height + u.px(12)
	x := (u.W - w) / 2
	y := u.contentBot() - h - u.px(10)
	c.Fill(x, y, w, h, colBG)
	c.Border(x, y, w, h, 1, colAmber)
	c.Text(u.fBody, x+u.px(14), y+u.px(6), u.toast, colAmber)
}

func (u *UI) drawModal(c *Canvas) {
	c.Blend(0, 0, u.W, u.H, colBG, 0.6)
	w := u.W * 80 / 100
	fs := cardFont(u.px(16), 0)
	var lines []string
	for _, l := range u.modalBody {
		lines = append(lines, fs.Wrap(l, w-u.px(48))...)
	}
	h := u.px(80) + len(lines)*fs.height
	x, y := (u.W-w)/2, (u.H-h)/2
	u.paper(c, x, y, w, h, false)
	c.TextSet(cardFont(u.px(20), 1), x+u.px(24), y+u.px(12), u.modalTitle, u.modalCol)
	c.Fill(x, y+u.px(42), w, 1, ruleRed)
	for i, l := range lines {
		c.TextSet(fs, x+u.px(24), y+u.px(54)+i*fs.height, l, inkCol)
	}
	c.TextRight(u.fTiny, x+w-u.px(12), y+h-u.fTiny.height-u.px(6), "A  OK", inkSoft)
}

// paper draws an index card with a soft shadow; ruled adds the red header line.
func (u *UI) paper(c *Canvas, x, y, w, h int, ruled bool) {
	c.Blend(x+u.px(5), y+u.px(6), w, h, colBlack, 0.45)
	c.Fill(x, y, w, h, paperCol)
	// a little tooth along the edges
	c.Fill(x, y, w, 1, mix(paperCol, colWhite, 0.5))
	c.Fill(x, y+h-1, w, 1, paperDim)
	c.Fill(x+w-1, y, 1, h, paperDim)
	if ruled {
		c.Fill(x, y+u.px(40), w, u.px(2), ruleRed)
	}
}

// ---------- library ----------

func (u *UI) libRowH() int { return u.px(64) }

func (u *UI) libRowsPerPage() int {
	return max(1, (u.contentBot()-u.contentTop()-u.px(20))/u.libRowH())
}

func (u *UI) drawLibrary(c *Canvas) {
	if len(u.lib) == 0 {
		u.drawEmpty(c)
		return
	}
	per := u.libRowsPerPage()
	if u.libSel < u.libTop {
		u.libTop = u.libSel
	}
	if u.libSel >= u.libTop+per {
		u.libTop = u.libSel - per + 1
	}
	x0, w := u.px(30), u.W-u.px(60)
	y := u.contentTop() + u.px(14)
	for i := u.libTop; i < len(u.lib) && i < u.libTop+per; i++ {
		e := u.lib[i]
		sel := i == u.libSel
		rh := u.libRowH() - u.px(8)
		x := x0
		if sel {
			x -= u.px(10) // pulled forward out of the drawer
		}
		col := paperCol
		if !sel {
			col = mix(paperCol, colDesk, 0.28)
		}
		c.Blend(x+u.px(4), y+u.px(5), w+u.px(10*b2i(sel)), rh, colBlack, 0.4)
		c.Fill(x, y, w+u.px(10*b2i(sel)), rh, col)
		ww := w + u.px(10*b2i(sel))
		// catalog tab with the first letter
		tabW := u.px(34)
		c.Fill(x, y, tabW, rh, mix(col, ruleRed, 0.12))
		c.Fill(x+tabW, y, 1, rh, mix(col, ruleRed, 0.5))
		r, _ := utf8.DecodeRuneInString(strings.ToUpper(e.Name()))
		tf := cardFont(u.px(24), 1)
		c.TextSet(tf, x+(tabW-tf.Width(string(r)))/2, y+(rh-tf.height)/2, string(r), mix(inkCol, ruleRed, 0.6))
		tx := x + tabW + u.px(14)
		nameF := cardFont(u.px(19), 1)
		subF := cardFont(u.px(14), 0)
		ink := inkCol
		if !sel {
			ink = mix(inkCol, col, 0.15)
		}
		countsW := u.px(170)
		c.TextSet(nameF, tx, y+u.px(8), nameF.Ellipsize(e.Name(), x+ww-countsW-tx), ink)
		sub := ""
		switch {
		case e.Err != "":
			sub = "CAN'T READ: " + e.Err
		case e.Pkg == nil:
			spin := []string{"·  ", "·· ", "···", " ··", "  ·", "   "}[u.frame/3%6]
			sub = e.Status + " " + spin
		default:
			nd := 0
			for _, d := range e.Pkg.Decks {
				if !strings.Contains(d.Name, "::") {
					nd++
				}
			}
			sub = fmt.Sprintf("%s CARDS", commas(len(e.Pkg.Cards)))
			if len(e.Pkg.Decks) > 1 {
				sub += fmt.Sprintf(" · %d DECKS", len(e.Pkg.Decks))
			}
			if len(e.Pkg.Media) > 0 {
				sub += fmt.Sprintf(" · %s MEDIA", commas(len(e.Pkg.Media)))
			}
			sub += " · " + filepath.Base(e.Path)
		}
		subCol := inkSoft
		if e.Err != "" {
			subCol = ruleRed
		}
		c.TextSet(subF, tx, y+u.px(34), subF.Ellipsize(sub, x+ww-countsW-tx), subCol)
		if e.Pkg != nil {
			u.drawCounts(c, x+ww-u.px(16), y+(rh-u.px(30))/2, e.Counts, sel)
		}
		y += u.libRowH()
	}
	if len(u.lib) > per {
		u.scrollbar(c, u.W-u.px(10), u.contentTop()+u.px(14), per*u.libRowH()-u.px(8), u.libTop, per, len(u.lib))
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// drawCounts draws NEW / LEARN / DUE numbers right-aligned at right.
func (u *UI) drawCounts(c *Canvas, right, y int, n Counts, bright bool) {
	big := cardFont(u.px(20), 1)
	small := cardFont(u.px(11), 0)
	items := []struct {
		v   int
		lbl string
		col color.RGBA
	}{{n.New, "NEW", colNew}, {n.Learn, "LEARN", colLearn}, {n.Due, "DUE", colDue}}
	colW := u.px(50)
	x := right - colW*3
	for _, it := range items {
		col := it.col
		if it.v == 0 {
			col = paperDim
		} else if !bright {
			col = mix(col, paperCol, 0.15)
		}
		s := fmt.Sprint(it.v)
		c.TextSet(big, x+(colW-big.Width(s))/2, y, s, col)
		c.TextSet(small, x+(colW-small.Width(it.lbl))/2, y+big.height-u.px(2), it.lbl, mix(col, paperCol, 0.25))
		x += colW
	}
}

func commas(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func (u *UI) scrollbar(c *Canvas, x, y, h, top, per, total int) {
	c.Fill(x, y, u.px(3), h, colGrid)
	th := max(u.px(14), h*per/total)
	ty := y + (h-th)*top/max(1, total-per)
	c.Fill(x, ty, u.px(3), th, colAmberDim)
}

const sharedDecksURL = "https://ankiweb.net/shared/decks"

func (u *UI) drawEmpty(c *Canvas) {
	x, y := u.px(30), u.contentTop()+u.px(18)
	w, h := u.W-u.px(60), u.contentBot()-y-u.px(16)
	u.paper(c, x, y, w, h, true)
	u.ruledLines(c, x, y+u.px(44), w, h-u.px(44), u.px(26), u.px(24))
	c.TextSet(cardFont(u.px(20), 1), x+u.px(22), y+u.px(10), "NO DECKS IN THE DRAWER YET", inkCol)
	qs := QRSize(sharedDecksURL, u.px(3))
	tw := w - qs - u.px(54)
	fs := cardFont(u.px(16), 0)
	var lines []string
	folder := "/mnt/SDCARD/Anki"
	if len(u.env.Config.DeckFolders) > 0 {
		folder = u.env.Config.DeckFolders[0]
	}
	folder = strings.TrimPrefix(folder, "/mnt/SDCARD/")
	for _, p := range []string{
		"1. On a computer, pick a shared deck at ankiweb.net/shared/decks (or export one of your own from Anki).",
		"2. Copy the .apkg file into the \"" + folder + "\" folder on this SD card.",
		"3. Press SELECT here (or reopen Flashback).",
	} {
		lines = append(lines, fs.Wrap(p, tw)...)
	}
	ty := y + u.px(42) + u.px(26) - fs.height + u.px(4)
	for _, l := range lines {
		c.TextSet(fs, x+u.px(22), ty, l, inkCol)
		ty += u.px(26)
	}
	qx, qy := x+w-qs-u.px(20), y+u.px(56)
	c.QR(qx, qy, u.px(3), sharedDecksURL)
	sf := cardFont(u.px(12), 0)
	cap := "SHARED DECKS"
	c.TextSet(sf, qx+(qs-sf.Width(cap))/2, qy+qs+u.px(4), cap, inkSoft)
}

// ruledLines draws the blue lines of an index card, offset by scroll.
// The lines fall at y + phase + k·gap (any integer k) inside [y, y+h).
func (u *UI) ruledLines(c *Canvas, x, y, w, h, gap, phase int) {
	if gap < 6 {
		return
	}
	first := ((phase % gap) + gap) % gap
	for ly := y + first; ly < y+h; ly += gap {
		c.Fill(x, ly, w, 1, ruleBlue)
	}
}

// ---------- decks (ledger sheet) ----------

func (u *UI) deckRowH() int { return u.px(36) }
func (u *UI) deckRowsPerPage() int {
	return max(1, (u.contentBot()-u.contentTop()-u.px(70))/u.deckRowH())
}

func (u *UI) sheet(c *Canvas) (x, y, w, h int) {
	x, y = u.px(18), u.contentTop()+u.px(12)
	w, h = u.W-u.px(36), u.contentBot()-y-u.px(10)
	u.paper(c, x, y, w, h, false)
	// ledger margin
	c.Fill(x+u.px(30), y, 1, h, mix(paperCol, ruleRed, 0.7))
	c.Fill(x+u.px(33), y, 1, h, mix(paperCol, ruleRed, 0.7))
	return
}

func (u *UI) drawDecks(c *Canvas) {
	x, y, w, h := u.sheet(c)
	hf := cardFont(u.px(12), 1)
	hy := y + u.px(12)
	c.TextSet(hf, x+u.px(46), hy, "DECK", inkSoft)
	colW := u.px(56)
	right := x + w - u.px(18)
	for i, l := range []string{"NEW", "LEARN", "DUE", "TOTAL"} {
		cx := right - colW*(4-i)
		c.TextSet(hf, cx+(colW-hf.Width(l))/2, hy, l, inkSoft)
	}
	top := y + u.px(34)
	c.Fill(x, top-u.px(2), w, u.px(2), ruleBlue)
	per := u.deckRowsPerPage()
	if u.deckSel < u.deckTop {
		u.deckTop = u.deckSel
	}
	if u.deckSel >= u.deckTop+per {
		u.deckTop = u.deckSel - per + 1
	}
	rh := u.deckRowH()
	nf := cardFont(u.px(18), 0)
	nb := cardFont(u.px(18), 1)
	vf := cardFont(u.px(18), 1)
	for i := u.deckTop; i < len(u.decks) && i < u.deckTop+per; i++ {
		d := u.decks[i]
		ry := top + (i-u.deckTop)*rh
		if i == u.deckSel {
			c.Fill(x+u.px(36), ry+u.px(4), w-u.px(44), rh-u.px(6), colMark)
		}
		c.Fill(x, ry+rh-1, w, 1, ruleBlue)
		lx := x + u.px(46) + max(0, d.Depth)*u.px(22)
		f := nf
		if d.Depth <= 0 {
			f = nb
		}
		label := d.Label
		if d.Depth > 0 {
			bx, by := lx-u.px(15), ry+u.px(8)
			c.Fill(bx, by, 1, rh/2-u.px(6), inkSoft)
			c.Fill(bx, by+rh/2-u.px(6), u.px(9), 1, inkSoft)
		}
		c.TextSet(f, lx, ry+(rh-f.height)/2, f.Ellipsize(label, right-colW*4-lx-u.px(8)), inkCol)
		vals := []int{d.Counts.New, d.Counts.Learn, d.Counts.Due, d.Counts.Total}
		cols := []color.RGBA{colNew, colLearn, colDue, inkSoft}
		for j, v := range vals {
			cx := right - colW*(4-j)
			s := commas(v)
			col := cols[j]
			if v == 0 {
				col = paperDim
			}
			ff := vf
			if j == 3 {
				ff = nf
			}
			c.TextSet(ff, cx+(colW-ff.Width(s))/2, ry+(rh-ff.height)/2, s, col)
		}
	}
	if len(u.decks) > per {
		u.scrollbar(c, x+w-u.px(8), top, per*rh, u.deckTop, per, len(u.decks))
	}
	// footnote: today's limits
	ff := cardFont(u.px(12), 0)
	note := fmt.Sprintf("Today: %d of %d new · %d of %d reviews · START for settings",
		u.cur.Prog.NewToday, u.env.Config.NewPerDay, u.cur.Prog.RevToday, u.env.Config.ReviewsPerDay)
	c.TextSet(ff, x+u.px(46), y+h-ff.height-u.px(8), ff.Ellipsize(note, w-u.px(60)), inkSoft)
}

// ---------- the card ----------

func (u *UI) cardRect() (x, y, w, h int) {
	x, y = u.px(22), u.contentTop()+u.px(12)
	return x, y, u.W - u.px(44), u.contentBot() - y - u.px(12)
}

func (u *UI) cardTextArea() (w, h int) {
	_, _, cw, ch := u.cardRect()
	return cw - u.px(52), ch - u.px(62)
}

func (u *UI) cardTextH() int { _, h := u.cardTextArea(); return h }

func (u *UI) lineGap() int { return cardFont(u.textSize(), 0).height + u.px(4) }

// gridBase is where a text baseline sits inside one ruled row.
func (u *UI) gridBase() int {
	fs := cardFont(u.textSize(), 0)
	return u.lineGap() - (fs.height - fs.ascent) - u.px(1)
}

func (u *UI) drawCardScreen(c *Canvas) {
	x, y, w, h := u.cardRect()
	if u.flipping() {
		// the card turns over: squash to an edge and open again
		t := float64(u.frame-u.flipStart) / flipFrames
		k := math.Abs(math.Cos(t * math.Pi))
		fw := max(u.px(4), int(float64(w)*k))
		fx := x + (w-fw)/2
		lift := int(math.Sin(t*math.Pi) * float64(u.px(10)))
		c.Blend(fx+u.px(8), y+u.px(10)+lift, fw, h, colBlack, 0.5)
		c.Fill(fx, y-lift, fw, h, paperCol)
		if t < 0.5 {
			c.Fill(fx, y-lift+u.px(40), fw, u.px(2), ruleRed)
		}
		return
	}
	u.paper(c, x, y, w, h, true)
	// header band: where this card is from, and the queue counts
	hf := cardFont(u.px(13), 1)
	label := u.deckLabel()
	side := "FRONT"
	if u.answer {
		side = "BACK"
	}
	if u.card >= 0 {
		c.TextSet(hf, x+u.px(18), y+u.px(13), hf.Ellipsize(strings.ToUpper(label), w/2), inkSoft)
	}
	right := x + w - u.px(18)
	if u.scr == scrStudy && u.sess != nil {
		n, l, d := u.sess.Remaining(u.now())
		kind := u.cur.Prog.kind(u.cur.Pkg.Cards[u.card].ID, u.now())
		items := []struct {
			v   int
			col color.RGBA
			on  bool
		}{{n, colNew, kind == kindNew}, {l, colLearn, kind == kindLearn}, {d, colDue, kind == kindDue}}
		bf := cardFont(u.px(16), 1)
		for i := len(items) - 1; i >= 0; i-- {
			it := items[i]
			s := fmt.Sprint(it.v)
			right -= bf.Width(s)
			c.TextSet(bf, right, y+u.px(11), s, it.col)
			if it.on {
				c.Fill(right, y+u.px(11)+bf.height, bf.Width(s), u.px(2), it.col)
			}
			right -= u.px(14)
		}
	} else if u.scr == scrView {
		s := fmt.Sprintf("%s / %s", commas(u.viewPos+1), commas(len(u.viewList)))
		right -= hf.Width(s)
		c.TextSet(hf, right, y+u.px(13), s, inkSoft)
		right -= u.px(14)
	}
	// FRONT/BACK tab
	tf := u.fTiny
	tw := tf.Width(side) + u.px(12)
	c.Border(right-tw, y+u.px(11), tw, tf.height+u.px(4), 1, inkSoft)
	c.Text(tf, right-tw+u.px(6), y+u.px(13), side, inkSoft)

	if u.layout == nil {
		return
	}
	tw2, th := u.cardTextArea()
	tx := x + u.px(30)
	ty := y + u.px(50)
	gap := u.lineGap()
	off := 0
	if u.layout.H < th {
		off = (th - u.layout.H) / 2
		off -= off % gap // sit on the ruled lines
	}
	// ruled lines sit just under each text baseline; keep clear of the red rule
	rs := y + u.px(52)
	firstLine := ty + off - u.cardScrl + u.gridBase() + u.px(2)
	u.ruledLines(c, x+u.px(2), rs, w-u.px(4), ty+th+u.px(8)-rs, gap, firstLine-rs)
	u.layout.Draw(c, tx, ty+off-u.cardScrl, y+u.px(44), ty+th+u.px(6))
	_ = tw2
	if ms := u.maxScroll(); ms > 0 {
		sx := x + w - u.px(9)
		c.Fill(sx, ty, u.px(3), th, paperDim)
		total := u.layout.H
		bh := max(u.px(16), th*th/total)
		by := ty + (th-bh)*u.cardScrl/ms
		c.Fill(sx, by, u.px(3), bh, inkSoft)
		if u.cardScrl < ms {
			c.TextCenter(u.fTiny, x+w/2, y+h-u.fTiny.height-u.px(2), "▼ MORE", inkSoft)
		}
	}
	if len(u.layout.Sounds) > 0 && !u.flipping() {
		sf := cardFont(u.px(11), 0)
		s := "♪ audio not played on this device"
		if u.env.Player != nil {
			s = "♪ START plays the sound"
		}
		c.TextSet(sf, x+u.px(18), y+h-sf.height-u.px(4), s, paperDim)
	}
}

// ---------- done ----------

func (u *UI) drawDone(c *Canvas) {
	x, y, w, h := u.cardRect()
	u.paper(c, x, y, w, h, true)
	u.ruledLines(c, x+u.px(2), y+u.px(50), w-u.px(4), h-u.px(56), u.px(30), u.px(30))
	hf := cardFont(u.px(13), 1)
	c.TextSet(hf, x+u.px(18), y+u.px(13), strings.ToUpper(u.deckLabel()), inkSoft)
	// rubber stamp
	sf := cardFont(u.px(34), 1)
	msg := "DONE FOR TODAY"
	sw := sf.Width(msg) + u.px(36)
	sh := sf.height + u.px(20)
	sx, sy := x+(w-sw)/2, y+u.px(80)
	stamp := rgb(0xc6, 0x2d, 0x2d)
	c.Border(sx, sy, sw, sh, u.px(3), stamp)
	c.Border(sx+u.px(6), sy+u.px(6), sw-u.px(12), sh-u.px(12), 1, stamp)
	c.TextSet(sf, sx+u.px(18), sy+u.px(10), msg, stamp)
	// speckle the stamp so it looks inked
	seed := uint32(97)
	for i := 0; i < 260; i++ {
		seed = seed*1664525 + 1013904223
		px := sx + int(seed>>8)%sw
		seed = seed*1664525 + 1013904223
		py := sy + int(seed>>8)%sh
		c.Set(px, py, paperCol)
	}
	bf := cardFont(u.px(18), 0)
	studied := 0
	if u.sess != nil {
		studied = u.sess.Done
	}
	lines := []string{}
	if studied > 0 {
		lines = append(lines, fmt.Sprintf("You finished %d card%s in this deck today.", studied, plural(studied)))
	} else {
		lines = append(lines, "Nothing due here right now.")
	}
	cnt := u.cur.Prog.Count(u.cur.Pkg, u.cur.Pkg.CardsIn(u.deckName), u.now().Add(24*time.Hour))
	if left := cnt.New; left > 0 {
		lines = append(lines, fmt.Sprintf("%s new card%s still waiting in the pile.", commas(left), plural(left)))
	}
	lines = append(lines, "See you tomorrow — or shuffle for fun.")
	ly := sy + sh + u.px(30)
	for _, l := range lines {
		for _, wl := range bf.Wrap(l, w-u.px(80)) {
			c.TextSet(bf, x+(w-bf.Width(wl))/2, ly, wl, inkCol)
			ly += u.px(30)
		}
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// ---------- browse ----------

func (u *UI) listRowH() int { return u.px(44) }
func (u *UI) listRowsPerPage() int {
	return max(1, (u.contentBot()-u.contentTop()-u.px(56))/u.listRowH())
}

func (u *UI) drawBrowse(c *Canvas) {
	x, y, w, h := u.sheet(c)
	hf := cardFont(u.px(12), 1)
	c.TextSet(hf, x+u.px(46), y+u.px(12), fmt.Sprintf("%s CARDS", commas(len(u.list))), inkSoft)
	pos := fmt.Sprintf("%s / %s", commas(min(len(u.list), u.listSel+1)), commas(len(u.list)))
	c.TextSet(hf, x+w-u.px(18)-hf.Width(pos), y+u.px(12), pos, inkSoft)
	top := y + u.px(34)
	c.Fill(x, top-u.px(2), w, u.px(2), ruleBlue)
	per := u.listRowsPerPage()
	if u.listSel < u.listTop {
		u.listTop = u.listSel
	}
	if u.listSel >= u.listTop+per {
		u.listTop = u.listSel - per + 1
	}
	rh := u.listRowH()
	qf := cardFont(u.px(17), 0)
	af := cardFont(u.px(13), 0)
	nf := cardFont(u.px(11), 0)
	tw := w - u.px(66)
	for i := u.listTop; i < len(u.list) && i < u.listTop+per; i++ {
		ry := top + (i-u.listTop)*rh
		if i == u.listSel {
			c.Fill(x+u.px(36), ry+u.px(3), w-u.px(44), rh-u.px(5), colMark)
		}
		c.Fill(x, ry+rh-1, w, 1, ruleBlue)
		num := fmt.Sprint(i + 1)
		c.TextSet(nf, x+u.px(28)-nf.Width(num), ry+u.px(6), num, paperDim)
		p := u.preview(u.list[i])
		c.TextSet(qf, x+u.px(46), ry+u.px(4), qf.Ellipsize(p[0], tw), inkCol)
		c.TextSet(af, x+u.px(46), ry+u.px(4)+qf.height, af.Ellipsize("→ "+p[1], tw), inkSoft)
	}
	if len(u.list) > per {
		u.scrollbar(c, x+w-u.px(8), top, per*rh, u.listTop, per, len(u.list))
	}
	_ = h
}
