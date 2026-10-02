package main

// A small HTML layout engine for card content. It understands the markup Anki
// fields actually contain — line breaks, divs and paragraphs, bold/italic/
// underline, colours, headings, lists, simple tables, images, horizontal rules,
// [sound:…] tags — and ignores CSS layout and scripts. Text wraps to the card
// width, with per-character breaking for CJK.

import (
	"bytes"
	"html"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Card palette: an index card on the desk.
var (
	inkCol    = rgb(0x1c, 0x24, 0x38) // typewriter ink
	inkSoft   = rgb(0x5d, 0x63, 0x70)
	paperCol  = rgb(0xf3, 0xec, 0xd8)
	ruleBlue  = rgb(0x9c, 0xb8, 0xd8)
	ruleRed   = rgb(0xd9, 0x4a, 0x3d)
	clozeCol  = rgb(0x1f, 0x56, 0xc8) // Anki's cloze blue, deepened for paper
	clozeBack = rgb(0xdc, 0xe6, 0xf6)
	hintCol   = rgb(0x8a, 0x6d, 0x3b)
)

type textStyle struct {
	size   int // pixel size
	bold   bool
	italic bool
	under  bool
	strike bool
	col    color.RGBA
	bg     *color.RGBA
	cloze  bool
}

func (s textStyle) font() *FontSet {
	st := 0
	if s.bold {
		st |= 1
	}
	if s.italic {
		st |= 2
	}
	return cardFont(s.size, st)
}

const (
	itText = iota
	itImage
	itRule
	itSound
)

type item struct {
	kind int
	x, w int
	h    int
	text string
	st   textStyle
	img  *image.RGBA
	io   string
	ans  bool // the <hr id=answer> divider
}

type line struct {
	items  []item
	y, h   int
	ascent int
}

type Layout struct {
	Lines   []line
	H       int
	W       int
	AnswerY int // y of the answer divider, or -1
	Sounds  []string
}

type MediaSource interface {
	ReadMedia(name string) ([]byte, error)
}

type word struct {
	text  string
	st    textStyle
	space bool // breakable space before this word
	kind  int
	img   *image.RGBA
	io    string
}

type layoutCtx struct {
	L        *Layout
	width    int
	base     int // base font size
	media    MediaSource
	maxImgH  int
	words    []word
	align    []string
	indent   int
	pendingS bool // a space is pending before the next word
	y        int
	pre      int
	imgCache map[string]*image.RGBA
	grid     int // when > 0, every line is a whole number of grid rows (ruled paper)
	gridBase int // baseline position inside a grid row
}

var soundRe = regexp.MustCompile(`\[sound:([^\]]+)\]`)

// LayoutHTML lays out card HTML into lines no wider than width. With grid > 0
// every line takes whole rows of that height and text baselines sit gridBase
// pixels into a row, so typed text lands on an index card's ruled lines.
func LayoutHTML(src string, width, baseSize, maxImgH, grid, gridBase int, media MediaSource) *Layout {
	lc := &layoutCtx{L: &Layout{W: width, AnswerY: -1}, width: width, base: baseSize, media: media, maxImgH: maxImgH, align: []string{"center"}, imgCache: map[string]*image.RGBA{}, grid: grid, gridBase: gridBase}
	st := textStyle{size: baseSize, col: inkCol}
	lc.run(src, st)
	lc.flush(false)
	// trim leading/trailing blank lines
	ls := lc.L.Lines
	for len(ls) > 0 && len(ls[len(ls)-1].items) == 0 {
		ls = ls[:len(ls)-1]
	}
	lc.L.Lines = ls
	if len(ls) > 0 {
		last := ls[len(ls)-1]
		lc.L.H = last.y + last.h
	}
	return lc.L
}

type stackEntry struct {
	tag    string
	st     textStyle
	hidden bool
	align  bool
	indent int
	list   *listState
	pre    bool
}

type listState struct {
	ordered bool
	n       int
}

var blockTags = map[string]bool{
	"div": true, "p": true, "li": true, "ul": true, "ol": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "tr": true, "table": true, "blockquote": true, "pre": true,
	"center": true, "section": true, "article": true, "dt": true, "dd": true, "dl": true, "figure": true,
	"figcaption": true, "header": true, "footer": true, "summary": true, "details": true, "thead": true,
	"tbody": true, "hr": true,
}

var hiddenTags = map[string]bool{"script": true, "style": true, "head": true, "title": true, "template": true, "canvas": true, "noscript": true, "audio": true, "video": true, "object": true, "iframe": true, "svg": true, "math": false}

var voidTags = map[string]bool{"br": true, "img": true, "hr": true, "input": true, "meta": true, "link": true, "wbr": true, "source": true, "col": true}

func (lc *layoutCtx) run(src string, base textStyle) {
	stack := []stackEntry{{tag: "#root", st: base}}
	top := func() *stackEntry { return &stack[len(stack)-1] }
	hidden := func() bool {
		for _, e := range stack {
			if e.hidden {
				return true
			}
		}
		return false
	}
	pop := func(tag string) {
		// pop to the matching tag, if open
		for i := len(stack) - 1; i > 0; i-- {
			if stack[i].tag == tag {
				for j := len(stack) - 1; j >= i; j-- {
					e := stack[j]
					if blockTags[e.tag] && !hidden() {
						lc.flush(false)
						if e.tag == "p" || strings.HasPrefix(e.tag, "h") && len(e.tag) == 2 || e.tag == "ul" || e.tag == "ol" || e.tag == "table" || e.tag == "blockquote" {
							lc.gap(lc.base / 3)
						}
					}
					if e.align {
						lc.align = lc.align[:len(lc.align)-1]
					}
					lc.indent -= e.indent
					if e.pre {
						lc.pre--
					}
				}
				stack = stack[:i]
				return
			}
		}
	}

	for len(src) > 0 {
		lt := strings.IndexByte(src, '<')
		if lt != 0 {
			text := src
			if lt > 0 {
				text = src[:lt]
				src = src[lt:]
			} else {
				src = ""
			}
			if !hidden() {
				lc.text(html.UnescapeString(text), top().st)
			}
			continue
		}
		if strings.HasPrefix(src, "<!--") {
			end := strings.Index(src, "-->")
			if end < 0 {
				return
			}
			src = src[end+3:]
			continue
		}
		gt := tagEnd(src)
		if gt < 0 {
			if !hidden() {
				lc.text(html.UnescapeString(src), top().st)
			}
			return
		}
		raw := src[1:gt]
		src = src[gt+1:]
		if raw == "" || raw[0] == '!' || raw[0] == '?' {
			continue
		}
		if raw[0] == '/' {
			pop(strings.ToLower(strings.TrimSpace(raw[1:])))
			continue
		}
		selfClose := strings.HasSuffix(raw, "/")
		raw = strings.TrimSuffix(raw, "/")
		name, attrs := parseTag(raw)
		if hiddenTags[name] {
			if voidTags[name] || selfClose {
				continue
			}
			// skip raw content of script/style
			if name == "script" || name == "style" {
				end := strings.Index(strings.ToLower(src), "</"+name)
				if end < 0 {
					return
				}
				src = src[end:]
				if e := strings.IndexByte(src, '>'); e >= 0 {
					src = src[e+1:]
				}
				continue
			}
		}
		if hidden() && !voidTags[name] && !selfClose {
			stack = append(stack, stackEntry{tag: name, st: top().st})
			continue
		}
		if hidden() {
			continue
		}
		e := stackEntry{tag: name, st: top().st}
		style := strings.ToLower(attrs["style"])
		css := parseCSS(style)
		if hiddenTags[name] || css["display"] == "none" || css["visibility"] == "hidden" || attrs["hidden"] != "" {
			e.hidden = true
		}
		applyStyle(&e.st, name, attrs, css, lc.base)
		if blockTags[name] && !e.hidden {
			lc.flush(false)
			if name == "p" || (len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6') {
				lc.gap(lc.base / 4)
			}
		}
		if a := css["text-align"]; a == "left" || a == "right" || a == "center" || a == "justify" {
			if a == "justify" {
				a = "left"
			}
			lc.align = append(lc.align, a)
			e.align = true
		} else if name == "center" {
			lc.align = append(lc.align, "center")
			e.align = true
		}
		switch name {
		case "br":
			if !e.hidden {
				lc.flush(true)
			}
			continue
		case "wbr":
			lc.pendingS = true
			continue
		case "hr":
			if !e.hidden {
				lc.flush(false)
				lc.rule(strings.EqualFold(attrs["id"], "answer"))
			}
			continue
		case "img":
			if !e.hidden {
				lc.image(attrs["src"], attrs["data-io"], attrs)
			}
			continue
		case "ul", "ol":
			e.list = &listState{ordered: name == "ol"}
			if s, ok := attrs["start"]; ok {
				n, _ := strconv.Atoi(s)
				e.list.n = n - 1
			}
			e.indent = lc.base
			lc.indent += e.indent
		case "li":
			var ls *listState
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].list != nil {
					ls = stack[i].list
					break
				}
			}
			lc.align = append(lc.align, "left")
			e.align = true
			bullet := "• "
			if ls != nil && ls.ordered {
				ls.n++
				bullet = strconv.Itoa(ls.n) + ". "
			}
			lc.words = append(lc.words, word{text: bullet, st: e.st, kind: itText})
			lc.pendingS = false
		case "blockquote":
			e.indent = lc.base
			lc.indent += e.indent
		case "pre":
			e.pre = true
			lc.pre++
			lc.align = append(lc.align, "left")
			e.align = true
		case "td", "th":
			lc.pendingS = true
			if len(lc.words) > 0 {
				lc.words = append(lc.words, word{text: "│", st: textStyle{size: e.st.size, col: inkSoft}, space: true})
				lc.pendingS = true
			}
		}
		if voidTags[name] || selfClose {
			if e.align {
				lc.align = lc.align[:len(lc.align)-1]
			}
			lc.indent -= e.indent
			if e.pre {
				lc.pre--
			}
			continue
		}
		stack = append(stack, e)
	}
}

// tagEnd finds the '>' closing a tag, skipping quoted attribute values.
func tagEnd(s string) int {
	q := byte(0)
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '>':
			return i
		}
	}
	return -1
}

func parseTag(raw string) (string, map[string]string) {
	attrs := map[string]string{}
	i := 0
	for i < len(raw) && !unicode.IsSpace(rune(raw[i])) {
		i++
	}
	name := strings.ToLower(raw[:i])
	rest := raw[i:]
	for {
		rest = strings.TrimLeft(rest, " \t\r\n")
		if rest == "" {
			break
		}
		j := 0
		for j < len(rest) && rest[j] != '=' && !unicode.IsSpace(rune(rest[j])) {
			j++
		}
		key := strings.ToLower(rest[:j])
		rest = strings.TrimLeft(rest[j:], " \t\r\n")
		val := ""
		if strings.HasPrefix(rest, "=") {
			rest = strings.TrimLeft(rest[1:], " \t\r\n")
			if len(rest) > 0 && (rest[0] == '"' || rest[0] == '\'') {
				q := rest[0]
				end := strings.IndexByte(rest[1:], q)
				if end < 0 {
					val, rest = rest[1:], ""
				} else {
					val, rest = rest[1:1+end], rest[2+end:]
				}
			} else {
				k := 0
				for k < len(rest) && !unicode.IsSpace(rune(rest[k])) {
					k++
				}
				val, rest = rest[:k], rest[k:]
			}
		} else if key == "" {
			break
		}
		if key != "" {
			attrs[key] = html.UnescapeString(val)
			if val == "" {
				attrs[key] = "1"
			}
		}
	}
	return name, attrs
}

func parseCSS(s string) map[string]string {
	m := map[string]string{}
	for _, decl := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(decl, ":")
		if ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
		}
	}
	return m
}

func applyStyle(st *textStyle, name string, attrs, css map[string]string, base int) {
	switch name {
	case "b", "strong":
		st.bold = true
	case "i", "em", "cite", "var", "dfn":
		st.italic = true
	case "u", "ins":
		st.under = true
	case "s", "strike", "del":
		st.strike = true
	case "h1":
		st.size, st.bold = base*3/2, true
	case "h2":
		st.size, st.bold = base*13/10, true
	case "h3", "h4":
		st.size, st.bold = base*23/20, true
	case "small", "sub", "sup", "rt", "h5", "h6":
		st.size = max(12, st.size*3/4)
	case "big":
		st.size = st.size * 5 / 4
	case "th":
		st.bold = true
	case "code", "kbd", "samp", "tt":
		// already monospace
	case "font":
		if c, ok := parseColor(attrs["color"]); ok {
			st.col = inkSafe(c)
		}
		if s := attrs["size"]; s != "" {
			n, _ := strconv.Atoi(strings.TrimLeft(s, "+"))
			if strings.HasPrefix(s, "+") {
				n += 3
			}
			if n >= 5 {
				st.size = base * 5 / 4
			} else if n > 0 && n <= 2 {
				st.size = base * 3 / 4
			}
		}
	}
	for _, cls := range strings.Fields(attrs["class"]) {
		switch cls {
		case "cloze":
			st.cloze, st.bold, st.col = true, true, clozeCol
		case "hint":
			st.italic, st.col = true, hintCol
		case "ruby":
			st.size, st.col = max(12, st.size*3/4), inkSoft
		case "typed":
			st.col = clozeCol
		}
	}
	if v := css["font-weight"]; v == "bold" || v == "bolder" || v == "600" || v == "700" || v == "800" || v == "900" {
		st.bold = true
	} else if v == "normal" || v == "400" {
		st.bold = false
	}
	if v := css["font-style"]; v == "italic" || v == "oblique" {
		st.italic = true
	}
	if v := css["text-decoration"]; strings.Contains(v, "underline") {
		st.under = true
	} else if strings.Contains(v, "line-through") {
		st.strike = true
	}
	if c, ok := parseColor(css["color"]); ok && !st.cloze {
		st.col = inkSafe(c)
	}
	bgv := css["background-color"]
	if bgv == "" {
		bgv = css["background"]
	}
	if c, ok := parseColor(bgv); ok && lum(c) < 0.97 && !(c.R == 0 && c.G == 0 && c.B == 0) {
		c = mix(paperCol, c, 0.55) // soften highlights so ink stays readable
		st.bg = &c
	}
	if v := css["font-size"]; v != "" {
		if px := cssSize(v, st.size, base); px > 0 {
			st.size = max(12, min(px, base*2))
		}
	}
}

func cssSize(v string, cur, base int) int {
	v = strings.TrimSpace(v)
	switch v {
	case "small", "smaller", "x-small", "xx-small":
		return cur * 3 / 4
	case "large", "larger":
		return cur * 5 / 4
	case "x-large", "xx-large":
		return cur * 3 / 2
	case "medium":
		return base
	}
	num := strings.TrimRight(v, "abcdefghijklmnopqrstuvwxyz%")
	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f <= 0 {
		return 0
	}
	switch {
	case strings.HasSuffix(v, "px"):
		// Anki's default card text is 20px; scale relative to that
		return int(f / 20 * float64(base))
	case strings.HasSuffix(v, "pt"):
		return int(f * 4 / 3 / 20 * float64(base))
	case strings.HasSuffix(v, "em"), strings.HasSuffix(v, "rem"):
		return int(f * float64(cur))
	case strings.HasSuffix(v, "%"):
		return int(f / 100 * float64(cur))
	}
	return 0
}

var namedColors = map[string]color.RGBA{
	"black": rgb(0, 0, 0), "white": rgb(255, 255, 255), "red": rgb(255, 0, 0), "green": rgb(0, 128, 0),
	"blue": rgb(0, 0, 255), "yellow": rgb(255, 255, 0), "orange": rgb(255, 165, 0), "purple": rgb(128, 0, 128),
	"gray": rgb(128, 128, 128), "grey": rgb(128, 128, 128), "brown": rgb(165, 42, 42), "pink": rgb(255, 192, 203),
	"navy": rgb(0, 0, 128), "teal": rgb(0, 128, 128), "maroon": rgb(128, 0, 0), "olive": rgb(128, 128, 0),
	"lime": rgb(0, 255, 0), "aqua": rgb(0, 255, 255), "cyan": rgb(0, 255, 255), "magenta": rgb(255, 0, 255),
	"fuchsia": rgb(255, 0, 255), "silver": rgb(192, 192, 192), "darkred": rgb(139, 0, 0), "darkgreen": rgb(0, 100, 0),
	"darkblue": rgb(0, 0, 139), "darkorange": rgb(255, 140, 0), "crimson": rgb(220, 20, 60), "gold": rgb(255, 215, 0),
	"indigo": rgb(75, 0, 130), "violet": rgb(238, 130, 238), "lightgreen": rgb(144, 238, 144), "lightblue": rgb(173, 216, 230),
}

func parseColor(s string) (color.RGBA, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "inherit" || s == "transparent" || s == "initial" || s == "none" {
		return color.RGBA{}, false
	}
	if c, ok := namedColors[s]; ok {
		return c, true
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 || len(h) == 4 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) >= 6 {
			v, err := strconv.ParseUint(h[:6], 16, 32)
			if err == nil {
				return rgb(uint8(v>>16), uint8(v>>8), uint8(v)), true
			}
		}
		return color.RGBA{}, false
	}
	if strings.HasPrefix(s, "rgb") {
		in := s[strings.IndexByte(s, '(')+1:]
		in = strings.TrimSuffix(in, ")")
		parts := strings.FieldsFunc(in, func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(parts) >= 3 {
			var v [3]uint8
			for i := 0; i < 3; i++ {
				f, _ := strconv.ParseFloat(strings.TrimSuffix(parts[i], "%"), 64)
				if strings.HasSuffix(parts[i], "%") {
					f = f * 2.55
				}
				v[i] = uint8(math.Max(0, math.Min(255, f)))
			}
			return rgb(v[0], v[1], v[2]), true
		}
	}
	return color.RGBA{}, false
}

func lum(c color.RGBA) float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}

// inkSafe darkens colours that would vanish on the paper (night-mode styles, yellows).
func inkSafe(c color.RGBA) color.RGBA {
	for lum(c) > 0.55 {
		c = mix(c, inkCol, 0.35)
	}
	return c
}

// ---- text and words ----

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) || (r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef)
}

func (lc *layoutCtx) text(s string, st textStyle) {
	// [sound:...] tags become speaker chips
	for {
		loc := soundRe.FindStringSubmatchIndex(s)
		if loc == nil {
			break
		}
		lc.textPlain(s[:loc[0]], st)
		lc.L.Sounds = append(lc.L.Sounds, s[loc[2]:loc[3]])
		if lc.pendingS {
			lc.pendingS = false
		}
		lc.words = append(lc.words, word{text: "♪", st: st, kind: itSound, space: len(lc.words) > 0})
		lc.pendingS = true
		s = s[loc[1]:]
	}
	lc.textPlain(s, st)
}

func (lc *layoutCtx) textPlain(s string, st textStyle) {
	if lc.pre > 0 {
		lines := strings.Split(s, "\n")
		for i, ln := range lines {
			if i > 0 {
				lc.flush(true)
			}
			if ln != "" {
				lc.words = append(lc.words, word{text: strings.ReplaceAll(ln, "\t", "    "), st: st})
			}
		}
		return
	}
	var cur strings.Builder
	emit := func() {
		if cur.Len() == 0 {
			return
		}
		lc.words = append(lc.words, word{text: cur.String(), st: st, space: lc.pendingS && len(lc.words) > 0})
		lc.pendingS = false
		cur.Reset()
	}
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '​':
			emit()
			lc.pendingS = true
		case isCJK(r):
			emit()
			lc.words = append(lc.words, word{text: string(r), st: st, space: lc.pendingS && len(lc.words) > 0})
			lc.pendingS = false
		default:
			cur.WriteRune(r)
		}
	}
	emit()
}

func (lc *layoutCtx) curAlign() string { return lc.align[len(lc.align)-1] }

func (lc *layoutCtx) gap(h int) {
	if len(lc.L.Lines) == 0 || lc.grid > 0 {
		return
	}
	lc.y += h
}

func (lc *layoutCtx) addLine(ln line) {
	if g := lc.grid; g > 0 {
		asc := ln.ascent
		if len(ln.items) == 1 && ln.items[0].kind == itRule {
			asc = 0
		}
		// enough rows for the tallest thing above the baseline; descenders
		// may dip below the ruled line, as they do on a typed card
		n := 1
		for (n-1)*g+lc.gridBase < asc && n < 200 {
			n++
		}
		ln.ascent = (n-1)*g + lc.gridBase
		ln.h = n * g
		for i := range ln.items {
			if ln.items[i].kind == itRule {
				ln.items[i].h = ln.h
			}
		}
	}
	ln.y = lc.y
	lc.L.Lines = append(lc.L.Lines, ln)
	lc.y += ln.h
}

// flush wraps the pending words into lines. With force, an empty block still
// produces a blank line (for <br>).
func (lc *layoutCtx) flush(force bool) {
	words := lc.words
	lc.words = nil
	lc.pendingS = false
	if len(words) == 0 {
		if force {
			fs := cardFont(lc.base, 0)
			lc.addLine(line{h: fs.height, ascent: fs.ascent})
		}
		return
	}
	avail := lc.width - lc.indent
	if avail < lc.width/3 {
		avail = lc.width / 3
	}
	align := lc.curAlign()
	var cur []item
	curW := 0
	emitLine := func() {
		ln := line{}
		asc, desc := 0, 0
		for _, it := range cur {
			if it.kind == itImage {
				asc = max(asc, it.h)
				continue
			}
			fs := it.st.font()
			asc = max(asc, fs.ascent)
			desc = max(desc, fs.height-fs.ascent)
		}
		if asc == 0 {
			fs := cardFont(lc.base, 0)
			asc, desc = fs.ascent, fs.height-fs.ascent
		}
		ln.ascent = asc
		ln.h = asc + desc
		off := lc.indent
		switch align {
		case "center":
			off += (avail - curW) / 2
		case "right":
			off += avail - curW
		}
		for i := range cur {
			cur[i].x += off
		}
		ln.items = cur
		lc.addLine(ln)
		cur = nil
		curW = 0
	}
	for _, w := range words {
		if w.kind == itImage {
			if len(cur) > 0 && curW+w.img.Bounds().Dx() > avail {
				emitLine()
			}
			x := curW
			if w.space && len(cur) > 0 {
				x += cardFont(lc.base, 0).Width(" ")
			}
			cur = append(cur, item{kind: itImage, x: x, w: w.img.Bounds().Dx(), h: w.img.Bounds().Dy(), img: w.img, io: w.io})
			curW = x + w.img.Bounds().Dx()
			continue
		}
		fs := w.st.font()
		ww := fs.Width(w.text)
		sp := 0
		if w.space && len(cur) > 0 {
			sp = fs.Width(" ")
		}
		if len(cur) > 0 && curW+sp+ww > avail {
			emitLine()
			sp = 0
		}
		text := w.text
		for ww > avail { // hard-break very long words (URLs, chemistry)
			cut := len(text)
			for cut > 1 && fs.Width(text[:cut]) > avail-curW {
				_, n := utf8.DecodeLastRuneInString(text[:cut])
				cut -= n
			}
			if cut <= 0 {
				cut = 1
			}
			cur = append(cur, item{kind: w.kind, x: curW + sp, w: fs.Width(text[:cut]), text: text[:cut], st: w.st})
			curW += sp + fs.Width(text[:cut])
			emitLine()
			sp = 0
			text = text[cut:]
			ww = fs.Width(text)
		}
		if text == "" {
			continue
		}
		cur = append(cur, item{kind: w.kind, x: curW + sp, w: ww, text: text, st: w.st})
		curW += sp + ww
	}
	if len(cur) > 0 {
		emitLine()
	}
}

func (lc *layoutCtx) rule(answer bool) {
	if answer {
		lc.L.AnswerY = lc.y
	}
	h := lc.base / 2
	lc.addLine(line{h: h, items: []item{{kind: itRule, x: 0, w: lc.width, h: h, ans: answer}}})
}

func (lc *layoutCtx) image(src, io string, attrs map[string]string) {
	if src == "" {
		return
	}
	name := src
	if u, err := urlUnescape(src); err == nil {
		name = u
	}
	img := lc.loadImage(name, attrs)
	if img == nil {
		lc.words = append(lc.words, word{text: "[" + shortName(name) + "]", st: textStyle{size: max(12, lc.base*3/4), col: inkSoft, italic: true}, space: true})
		return
	}
	lc.words = append(lc.words, word{kind: itImage, img: img, io: io, space: lc.pendingS && len(lc.words) > 0})
	lc.pendingS = false
}

func shortName(s string) string {
	if len(s) > 24 {
		return s[:21] + "…"
	}
	return s
}

func urlUnescape(s string) (string, error) {
	if !strings.Contains(s, "%") {
		return s, nil
	}
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				return s, err
			}
			b.WriteByte(byte(v))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

var imageCache = struct {
	keys []string
	m    map[string]*image.RGBA
}{m: map[string]*image.RGBA{}}

func (lc *layoutCtx) loadImage(name string, attrs map[string]string) *image.RGBA {
	if lc.media == nil {
		return nil
	}
	maxW, maxH := lc.width-lc.indent, lc.maxImgH
	if v, err := strconv.Atoi(strings.TrimSuffix(attrs["width"], "px")); err == nil && v > 0 {
		maxW = min(maxW, max(16, v*lc.base/20))
	}
	if v, err := strconv.Atoi(strings.TrimSuffix(attrs["height"], "px")); err == nil && v > 0 {
		maxH = min(maxH, max(16, v*lc.base/20))
	}
	key := name + "|" + strconv.Itoa(maxW) + "x" + strconv.Itoa(maxH)
	if im, ok := imageCache.m[key]; ok {
		return im
	}
	b, err := lc.media.ReadMedia(name)
	if err != nil {
		return nil
	}
	if looksLikeSVG(name, b) {
		im, err := RenderSVG(b, maxW, maxH, 3)
		if err != nil {
			return nil
		}
		imageCache.m[key] = im
		imageCache.keys = append(imageCache.keys, key)
		return im
	}
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw == 0 || sh == 0 {
		return nil
	}
	// fit inside maxW×maxH; small images are enlarged up to 2× for legibility
	scale := math.Min(float64(maxW)/float64(sw), float64(maxH)/float64(sh))
	scale = math.Min(scale, 2)
	w, h := max(1, int(float64(sw)*scale)), max(1, int(float64(sh)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// paper behind transparent images
	for i := 0; i < len(dst.Pix); i += 4 {
		dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = 255, 255, 255, 255
	}
	if scale < 1 {
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, sb, xdraw.Over, nil)
	} else {
		xdraw.NearestNeighbor.Scale(dst, dst.Bounds(), src, sb, xdraw.Over, nil)
	}
	imageCache.m[key] = dst
	imageCache.keys = append(imageCache.keys, key)
	if len(imageCache.keys) > 24 {
		delete(imageCache.m, imageCache.keys[0])
		imageCache.keys = imageCache.keys[1:]
	}
	return dst
}

// ---- drawing ----

// Draw renders the layout with its top-left at (x, y), clipped to [clipTop, clipBottom).
func (L *Layout) Draw(c *Canvas, x, y, clipTop, clipBottom int) {
	for _, ln := range L.Lines {
		ly := y + ln.y
		if ly+ln.h < clipTop || ly > clipBottom {
			continue
		}
		for _, it := range ln.items {
			switch it.kind {
			case itText, itSound:
				fs := it.st.font()
				ty := ly + ln.ascent - fs.ascent
				if ty < clipTop-fs.height/2 || ty+fs.height > clipBottom+fs.height/2 {
					continue
				}
				if it.st.bg != nil {
					c.Fill(x+it.x-2, ty, it.w+4, fs.height, *it.st.bg)
				}
				if it.kind == itSound {
					drawSpeaker(c, x+it.x, ty, fs.height, it.st.col)
					continue
				}
				if it.st.cloze {
					c.Fill(x+it.x-3, ty+1, it.w+6, fs.height-1, clozeBack)
				}
				c.TextSet(fs, x+it.x, ty, it.text, it.st.col)
				if it.st.under {
					c.Fill(x+it.x, ty+fs.ascent+2, it.w, 1, it.st.col)
				}
				if it.st.strike {
					c.Fill(x+it.x, ty+fs.ascent*2/3, it.w, 1, it.st.col)
				}
			case itImage:
				iy := ly + ln.ascent - it.h
				drawClipped(c, it.img, x+it.x, iy, clipTop, clipBottom)
				frame := rgb(0xc9, 0xbf, 0xa4)
				fx, fy, fw, fh := x+it.x-1, iy-1, it.w+2, it.h+2
				for _, r := range [][4]int{{fx, fy, fw, 1}, {fx, fy + fh - 1, fw, 1}, {fx, fy, 1, fh}, {fx + fw - 1, fy, 1, fh}} {
					top, bot := max(r[1], clipTop), min(r[1]+r[3], clipBottom)
					if bot > top {
						c.Fill(r[0], top, r[2], bot-top, frame)
					}
				}
				if it.io != "" {
					drawOcclusions(c, it.io, x+it.x, iy, it.w, it.h, clipTop, clipBottom)
				}
			case itRule:
				ry := ly + ln.h/2
				if ry < clipTop || ry >= clipBottom {
					continue
				}
				if it.ans {
					c.Fill(x, ry-2, L.W, 1, ruleRed)
					c.Fill(x, ry+1, L.W, 1, ruleRed)
				} else {
					c.HLineDashed(x, ry, L.W, 6, 4, inkSoft)
				}
			}
		}
	}
}

func drawClipped(c *Canvas, img *image.RGBA, x, y, top, bottom int) {
	b := img.Bounds()
	for yy := 0; yy < b.Dy(); yy++ {
		py := y + yy
		if py < top || py >= bottom || py < 0 || py >= c.H {
			continue
		}
		so := yy * img.Stride
		for xx := 0; xx < b.Dx(); xx++ {
			px := x + xx
			if px < 0 || px >= c.W {
				continue
			}
			do := py*c.Stride + px*4
			copy(c.Pix[do:do+4], img.Pix[so+xx*4:so+xx*4+4])
		}
	}
}

func drawSpeaker(c *Canvas, x, y, h int, col color.RGBA) {
	s := h * 6 / 10
	cy := y + h/2
	c.Fill(x+1, cy-s/6, s/3, s/3+1, col)
	for i := 0; i < s/3; i++ {
		c.Fill(x+1+s/3+i, cy-s/6-i, 1, s/3+1+2*i, col)
	}
	for k := 1; k <= 2; k++ {
		r := s/3 + k*s/5
		for a := -40; a <= 40; a += 5 {
			rad := float64(a) * math.Pi / 180
			c.Set(x+s/2+int(float64(r)*math.Cos(rad)), cy+int(float64(r)*math.Sin(rad)), col)
		}
	}
}

// drawOcclusions paints image-occlusion masks: "a" = the one being asked (red),
// "i" = other hidden regions (amber), "r" = revealed (outline only).
func drawOcclusions(c *Canvas, spec string, x, y, w, h, top, bottom int) {
	for _, sh := range strings.Split(spec, ";") {
		parts := strings.SplitN(sh, "|", 3)
		if len(parts) != 3 {
			continue
		}
		var nums []float64
		for _, s := range strings.Split(parts[2], ",") {
			f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				f = 0
			}
			nums = append(nums, f)
		}
		fill := rgb(0xff, 0x8e, 0x8e)
		edge := rgb(0xd3, 0x2f, 0x2f)
		switch parts[0] {
		case "i":
			fill, edge = rgb(0xff, 0xeb, 0xa2), rgb(0xb0, 0x8b, 0x1e)
		case "r":
			edge = clozeCol
		}
		inside := func(px, py float64) bool { return false }
		switch parts[1] {
		case "rect":
			if len(nums) < 4 {
				continue
			}
			l, t, ww, hh := nums[0], nums[1], nums[2], nums[3]
			inside = func(px, py float64) bool { return px >= l && px <= l+ww && py >= t && py <= t+hh }
		case "ellipse":
			if len(nums) < 4 || nums[2] == 0 || nums[3] == 0 {
				continue
			}
			cx, cy, rx, ry := nums[0]+nums[2], nums[1]+nums[3], nums[2], nums[3]
			inside = func(px, py float64) bool {
				dx, dy := (px-cx)/rx, (py-cy)/ry
				return dx*dx+dy*dy <= 1
			}
		case "polygon":
			if len(nums) < 6 {
				continue
			}
			pts := nums
			inside = func(px, py float64) bool {
				in := false
				n := len(pts) / 2
				for i, j := 0, n-1; i < n; j, i = i, i+1 {
					xi, yi, xj, yj := pts[2*i], pts[2*i+1], pts[2*j], pts[2*j+1]
					if (yi > py) != (yj > py) && px < (xj-xi)*(py-yi)/(yj-yi)+xi {
						in = !in
					}
				}
				return in
			}
		default:
			continue
		}
		for yy := 0; yy < h; yy++ {
			py := y + yy
			if py < top || py >= bottom {
				continue
			}
			fy := (float64(yy) + 0.5) / float64(h)
			for xx := 0; xx < w; xx++ {
				fx := (float64(xx) + 0.5) / float64(w)
				if !inside(fx, fy) {
					continue
				}
				// edge = any neighbour outside
				d := 1.5
				isEdge := !inside(fx-d/float64(w), fy) || !inside(fx+d/float64(w), fy) || !inside(fx, fy-d/float64(h)) || !inside(fx, fy+d/float64(h))
				switch {
				case isEdge:
					c.Set(x+xx, py, edge)
				case parts[0] != "r":
					c.Set(x+xx, py, fill)
				}
			}
		}
	}
}

// stripHTML returns the plain text of an HTML fragment.
func stripHTML(s string) string { return plainText(s, " ", "") }

// previewText is plain text for list rows: blocks separated by " · ", images as ▣.
func previewText(s string) string {
	t := plainText(s, " · ", " ▣ ")
	parts := strings.Split(t, "·")
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

func plainText(s, blockSep, imgMark string) string {
	var b strings.Builder
	for len(s) > 0 {
		lt := strings.IndexByte(s, '<')
		if lt < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:lt])
		s = s[lt:]
		gt := tagEnd(s)
		if gt < 0 {
			break
		}
		name, _ := parseTag(strings.TrimPrefix(s[1:gt], "/"))
		if (name == "script" || name == "style") && s[1] != '/' {
			end := strings.Index(strings.ToLower(s), "</"+name)
			if end < 0 {
				break
			}
			s = s[end:]
			continue
		}
		switch {
		case blockTags[name] || name == "br":
			b.WriteString(blockSep)
		case name == "img" && imgMark != "":
			b.WriteString(imgMark)
		}
		s = s[gt+1:]
	}
	out := html.UnescapeString(b.String())
	out = soundRe.ReplaceAllString(out, " ♪ ")
	return strings.Join(strings.Fields(strings.ReplaceAll(out, " ", " ")), " ")
}
