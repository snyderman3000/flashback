package main

// Anki card templates: {{Field}}, {{#Field}}…{{/Field}}, {{^Field}}…{{/Field}},
// {{FrontSide}}, filters like {{cloze:Text}}, {{type:Back}}, {{hint:X}},
// {{text:X}}, {{furigana:X}}/{{kana:X}}/{{kanji:X}}, {{tts …:X}}, plus the
// special fields Tags, Deck, Subdeck, Card, Type.

import (
	"fmt"
	"html"
	"log"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
)

// CardHTML renders the question or answer side of a card to HTML. A card
// that can't be rendered shows an explanation instead of taking the app down.
func (p *Package) CardHTML(ci int, answer bool) (out string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("rendering card %d (answer=%v): %v\n%s", ci, answer, r, debug.Stack())
			out = "<i>Flashback couldn't show this card.</i><br><small>" + html.EscapeString(fmt.Sprint(r)) + "</small>"
		}
	}()
	return p.cardHTML(ci, answer)
}

func (p *Package) cardHTML(ci int, answer bool) string {
	c := p.Cards[ci]
	n := p.Notes[c.Note]
	nt := p.Notetypes[n.Mid]
	flds := p.Fields(c.Note)
	if nt == nil {
		return "<i>(this card's note type is missing from the package)</i><br>" + strings.Join(flds, "<br>")
	}
	ctx := &tmplCtx{p: p, nt: nt, card: c, flds: flds, ord: c.Ord, tags: n.Tags}
	t := nt.templateFor(c.Ord)
	q := ctx.render(t.Q, false, "")
	var out string
	if !nt.isImageOcclusion() && !visibleHTML(q) {
		// The template builds the card with JavaScript (e.g. "Cloze Overlapping")
		// and leaves only hidden fields behind. Show the note's content directly.
		out = ctx.fallback(t, answer)
	} else if answer {
		out = ctx.render(t.A, true, q)
	} else {
		out = q
	}
	if nt.isImageOcclusion() {
		out = ctx.imageOcclusion(out, answer)
	}
	return out
}

var clozeTagRe = regexp.MustCompile(`\{\{[^{}]*\bcloze:([^{}:]+)\}\}`)

// fallback renders a card whose template shows nothing without JavaScript:
// the cloze field (or the first non-empty field) as the question, and on the
// answer side the revealed text plus whatever the answer template does show.
func (x *tmplCtx) fallback(t Template, answer bool) string {
	var q, a string
	if m := clozeTagRe.FindStringSubmatch(t.Q + t.A); m != nil {
		v, _ := x.field(strings.TrimSpace(m[1]))
		q = renderCloze(v, x.ord+1, false)
		a = renderCloze(v, x.ord+1, true)
	} else {
		for i, f := range x.flds {
			if nonEmpty(f) && i < len(x.nt.Fields) {
				q, a = f, f
				break
			}
		}
	}
	if !answer {
		return q
	}
	extra := x.render(t.A, true, "")
	if !visibleHTML(extra) {
		// nothing visible on the back either: list the other filled-in fields
		var parts []string
		for i, f := range x.flds {
			if nonEmpty(f) && f != q && i < len(x.nt.Fields) && !strings.Contains(a, f) {
				parts = append(parts, f)
			}
		}
		extra = strings.Join(parts, "<br>")
	}
	return a + "<hr id=answer>" + extra
}

// visibleHTML reports whether HTML would show anything on the card.
func visibleHTML(h string) bool {
	if strings.TrimSpace(h) == "" {
		return false
	}
	return len(LayoutHTML(h, 4000, 20, 100, 0, 0, nil).Lines) > 0
}

func (nt *Notetype) templateFor(ord int) Template {
	if len(nt.Templates) == 0 {
		return Template{Q: "{{" + nt.firstField() + "}}", A: "{{FrontSide}}<hr id=answer>"}
	}
	if nt.Cloze || ord >= len(nt.Templates) || ord < 0 {
		return nt.Templates[0]
	}
	return nt.Templates[ord]
}

func (nt *Notetype) firstField() string {
	if len(nt.Fields) > 0 {
		return nt.Fields[0]
	}
	return ""
}

func (nt *Notetype) isImageOcclusion() bool {
	for _, t := range nt.Templates {
		if strings.Contains(t.Q, "image-occlusion") {
			return true
		}
	}
	return false
}

type tmplCtx struct {
	p    *Package
	nt   *Notetype
	card CardRef
	flds []string
	ord  int
	tags string
}

func (x *tmplCtx) field(name string) (string, bool) {
	for i, f := range x.nt.Fields {
		if f == name && i < len(x.flds) {
			return x.flds[i], true
		}
	}
	for i, f := range x.nt.Fields {
		if strings.EqualFold(f, name) && i < len(x.flds) {
			return x.flds[i], true
		}
	}
	return "", false
}

func (x *tmplCtx) deckName() string {
	for _, d := range x.p.Decks {
		if d.ID == x.card.Deck {
			return d.Name
		}
	}
	return ""
}

// special returns the value of a built-in field.
func (x *tmplCtx) special(name string, front string) (string, bool) {
	switch name {
	case "FrontSide":
		return front, true
	case "Tags":
		return x.tags, true
	case "Deck":
		return x.deckName(), true
	case "Subdeck":
		parts := strings.Split(x.deckName(), "::")
		return parts[len(parts)-1], true
	case "Type":
		return x.nt.Name, true
	case "Card":
		return x.nt.templateFor(x.ord).Name, true
	case "CardFlag":
		return "", true
	}
	return "", false
}

var tagRe = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

func (x *tmplCtx) render(tmpl string, answer bool, front string) string {
	var sb strings.Builder
	x.renderInto(&sb, tmpl, answer, front, 0)
	return sb.String()
}

func (x *tmplCtx) renderInto(sb *strings.Builder, tmpl string, answer bool, front string, depth int) {
	if depth > 20 {
		return
	}
	for len(tmpl) > 0 {
		loc := tagRe.FindStringSubmatchIndex(tmpl)
		if loc == nil {
			sb.WriteString(tmpl)
			return
		}
		sb.WriteString(tmpl[:loc[0]])
		tag := strings.TrimSpace(tmpl[loc[2]:loc[3]])
		rest := tmpl[loc[1]:]
		if len(tag) > 0 && (tag[0] == '#' || tag[0] == '^') {
			name := strings.TrimSpace(tag[1:])
			body, after := splitSection(rest, name)
			if x.truthy(name, front) == (tag[0] == '#') {
				x.renderInto(sb, body, answer, front, depth+1)
			}
			tmpl = after
			continue
		}
		if len(tag) > 0 && tag[0] == '/' {
			tmpl = rest // stray close tag
			continue
		}
		sb.WriteString(x.replace(tag, answer, front))
		tmpl = rest
	}
}

// splitSection finds the matching {{/name}} (respecting nesting of the same name).
func splitSection(s, name string) (body, after string) {
	depth := 0
	pos := 0
	for {
		loc := tagRe.FindStringSubmatchIndex(s[pos:])
		if loc == nil {
			return s, ""
		}
		tag := strings.TrimSpace(s[pos+loc[2] : pos+loc[3]])
		if len(tag) > 1 && (tag[0] == '#' || tag[0] == '^') && strings.TrimSpace(tag[1:]) == name {
			depth++
		} else if len(tag) > 1 && tag[0] == '/' && strings.TrimSpace(tag[1:]) == name {
			if depth == 0 {
				return s[:pos+loc[0]], s[pos+loc[1]:]
			}
			depth--
		}
		pos += loc[1]
	}
}

func (x *tmplCtx) truthy(name, front string) bool {
	if v, ok := x.special(name, front); ok {
		return nonEmpty(v)
	}
	// {{#c1}} sections in cloze note types: true when that cloze is the card's
	if strings.HasPrefix(name, "c") {
		if n, err := strconv.Atoi(name[1:]); err == nil && x.nt.Cloze {
			return n == x.ord+1
		}
	}
	v, _ := x.field(name)
	return nonEmpty(v)
}

// emptyFieldRe matches what Anki itself treats as an empty field: only
// whitespace and <br>/<div> tags. An image on its own is not empty.
var emptyFieldRe = regexp.MustCompile(`(?is)^(?:\s|\x{200b}|\x{a0}|&nbsp;|</?(?:br|div)\s*/?>)*$`)

func nonEmpty(v string) bool {
	return !emptyFieldRe.MatchString(v)
}

func (x *tmplCtx) replace(tag string, answer bool, front string) string {
	parts := strings.Split(tag, ":")
	name := strings.TrimSpace(parts[len(parts)-1])
	filters := parts[:len(parts)-1]
	var val string
	if v, ok := x.special(name, front); ok {
		val = v
	} else if v, ok := x.field(name); ok {
		val = v
	} else {
		return ""
	}
	// type-in-the-answer: nothing to type on a handheld, so the question shows
	// nothing and the answer shows the expected text
	for _, f := range filters {
		if strings.TrimSpace(f) == "type" {
			if !answer {
				return ""
			}
			for _, g := range filters {
				if strings.TrimSpace(g) == "cloze" {
					return `<div class="typed">` + clozeOnly(val, x.ord+1) + `</div>`
				}
			}
			return `<div class="typed">` + val + `</div>`
		}
	}
	// filters apply innermost (nearest the field name) first
	for i := len(filters) - 1; i >= 0; i-- {
		f := strings.TrimSpace(filters[i])
		if f == "" {
			continue // "{{clickable::Tags}}" and similar double-colon spellings
		}
		fname := strings.Fields(f)[0]
		switch fname {
		case "cloze":
			val = renderCloze(val, x.ord+1, answer)
		case "cloze-only":
			val = clozeOnly(val, x.ord+1)
		case "hint":
			if nonEmpty(val) {
				val = `<div class="hint">` + val + `</div>`
			}
		case "text":
			val = html.EscapeString(stripHTML(val))
		case "furigana":
			val = furigana(val, "furigana")
		case "kana":
			val = furigana(val, "kana")
		case "kanji":
			val = furigana(val, "kanji")
		case "tts", "tts-voices":
			// spoken by Anki; we just show the text
		}
	}
	return val
}

// ---- cloze ----

// renderCloze replaces {{cN::text::hint}} markers. The active cloze (n) is shown
// as [...]/[hint] on the question and highlighted on the answer; the others show
// their text. Nested clozes are handled.
func renderCloze(s string, n int, answer bool) string {
	return clozeWalk(s, n, answer)
}

func clozeOnly(s string, n int) string {
	var found []string
	collectCloze(s, n, &found)
	return strings.Join(found, ", ")
}

func collectCloze(s string, n int, found *[]string) {
	for i := 0; i < len(s); i++ {
		if num, start, ok := clozeOpen(s, i); ok {
			body, end := clozeBody(s, start)
			text, _ := splitHint(body)
			if num == n {
				*found = append(*found, renderCloze(text, n, true))
			}
			collectCloze(text, n, found)
			i = end - 1
		}
	}
}

// clozeOpen reports whether a "{{cN::" marker starts at i.
func clozeOpen(s string, i int) (num int, bodyStart int, ok bool) {
	if !strings.HasPrefix(s[i:], "{{c") {
		return 0, 0, false
	}
	j := i + 3
	k := j
	for k < len(s) && s[k] >= '0' && s[k] <= '9' {
		k++
	}
	if k == j || !strings.HasPrefix(s[k:], "::") {
		return 0, 0, false
	}
	num, _ = strconv.Atoi(s[j:k])
	return num, k + 2, true
}

// clozeBody returns the body of a cloze starting at start and the index after its closing }}.
func clozeBody(s string, start int) (string, int) {
	depth := 0
	for i := start; i < len(s); i++ {
		if _, _, ok := clozeOpen(s, i); ok {
			depth++
			i += 4
			continue
		}
		if strings.HasPrefix(s[i:], "}}") {
			if depth == 0 {
				return s[start:i], i + 2
			}
			depth--
			i++
		}
	}
	return s[start:], len(s)
}

func splitHint(body string) (text, hint string) {
	// the hint is after the last top-level "::"
	depth := 0
	last := -1
	for i := 0; i < len(body); i++ {
		if _, _, ok := clozeOpen(body, i); ok {
			depth++
			i += 4
			continue
		}
		if strings.HasPrefix(body[i:], "}}") && depth > 0 {
			depth--
			i++
			continue
		}
		if depth == 0 && strings.HasPrefix(body[i:], "::") {
			last = i
			i++
		}
	}
	if last < 0 {
		return body, ""
	}
	return body[:last], body[last+2:]
}

func clozeWalk(s string, n int, answer bool) string {
	var sb strings.Builder
	for i := 0; i < len(s); {
		num, start, ok := clozeOpen(s, i)
		if !ok {
			sb.WriteByte(s[i])
			i++
			continue
		}
		body, end := clozeBody(s, start)
		text, hint := splitHint(body)
		inner := clozeWalk(text, n, answer)
		switch {
		case num == n && !answer:
			label := "[...]"
			if hint != "" {
				label = "[" + hint + "]"
			}
			sb.WriteString(`<span class="cloze">` + label + `</span>`)
		case num == n:
			sb.WriteString(`<span class="cloze">` + inner + `</span>`)
		default:
			sb.WriteString(inner)
		}
		i = end
	}
	return sb.String()
}

// ClozeNumbers lists the cloze numbers used in a field.
func ClozeNumbers(s string) []int {
	seen := map[int]bool{}
	var out []int
	for i := 0; i < len(s); i++ {
		if num, _, ok := clozeOpen(s, i); ok && !seen[num] {
			seen[num] = true
			out = append(out, num)
		}
	}
	return out
}

// ---- furigana: "日本語[にほんご]" ----

var furiRe = regexp.MustCompile(` ?([^ >\[]+?)\[(.+?)\]`)

func furigana(s, mode string) string {
	return furiRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := furiRe.FindStringSubmatch(m)
		base, reading := sub[1], sub[2]
		if strings.HasPrefix(reading, "sound:") {
			return m
		}
		switch mode {
		case "kana":
			return reading
		case "kanji":
			return base
		}
		return base + `<span class="ruby">(` + reading + `)</span>`
	})
}

// ---- image occlusion (Anki 23.10+) ----
// The Occlusion field holds {{cN::image-occlusion:rect:left=.1:top=.2:width=.3:height=.1:oi=1}}
// shapes; Anki draws them over the image with JavaScript. We pass them to the
// layout as a data attribute on the image instead.

type ioShape struct {
	Kind   string // rect, ellipse, polygon, text
	Cloze  int
	Vals   map[string]string
	Hidden bool // "oi=1": inactive shapes stay masked ("hide all, guess one")
}

func parseOcclusions(s string) []ioShape {
	var out []ioShape
	for i := 0; i < len(s); i++ {
		num, start, ok := clozeOpen(s, i)
		if !ok {
			continue
		}
		body, end := clozeBody(s, start)
		body = strings.TrimPrefix(html.UnescapeString(body), "image-occlusion:")
		parts := strings.Split(body, ":")
		if len(parts) > 1 {
			sh := ioShape{Kind: parts[0], Cloze: num, Vals: map[string]string{}}
			for _, kv := range parts[1:] {
				if k, v, ok := strings.Cut(kv, "="); ok {
					sh.Vals[k] = v
				}
			}
			sh.Hidden = sh.Vals["oi"] == "1"
			out = append(out, sh)
		}
		i = end - 1
	}
	return out
}

// imageOcclusion attaches the masks for this card to the first image.
func (x *tmplCtx) imageOcclusion(out string, answer bool) string {
	occ, _ := x.field("Occlusion")
	if occ == "" && len(x.flds) > 0 {
		occ = x.flds[0]
	}
	shapes := parseOcclusions(occ)
	var spec []string
	for _, sh := range shapes {
		mode := ""
		switch {
		case sh.Cloze == x.ord+1 && !answer:
			mode = "a" // the one to guess
		case sh.Cloze == x.ord+1:
			mode = "r" // revealed: outline only
		case sh.Hidden:
			mode = "i" // other masks stay on
		default:
			continue
		}
		var nums []string
		switch sh.Kind {
		case "rect":
			nums = []string{sh.Vals["left"], sh.Vals["top"], sh.Vals["width"], sh.Vals["height"]}
		case "ellipse":
			nums = []string{sh.Vals["left"], sh.Vals["top"], sh.Vals["rx"], sh.Vals["ry"]}
		case "polygon":
			nums = strings.FieldsFunc(sh.Vals["points"], func(r rune) bool { return r == ' ' || r == ',' })
		default:
			continue
		}
		spec = append(spec, fmt.Sprintf("%s|%s|%s", mode, sh.Kind, strings.Join(nums, ",")))
	}
	if len(spec) == 0 {
		return out
	}
	attr := `<img data-io="` + html.EscapeString(strings.Join(spec, ";")) + `" `
	if i := strings.Index(strings.ToLower(out), "<img "); i >= 0 {
		return out[:i] + attr + out[i+5:]
	}
	return out
}
