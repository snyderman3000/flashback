package main

// Just enough CSS to hide what a card's stylesheet hides. Note types lean on
// CSS to switch parts of a template off: AnKing cards print the cloze twice and
// hide one copy, keep the tag list in a hidden container, park a Wikipedia
// popup off screen, and so on. Rules that set display are applied to the
// rendered card HTML; every other property is still ignored.
//
// Selectors: type, *, .class, #id, [attr], [attr=value], compounds of those,
// and descendant / child combinators. A selector with anything else in it
// (pseudo-classes, sibling combinators, other attribute operators) never
// matches, and @-rule blocks such as @media are skipped. Cards are matched the
// way Anki's mobile clients show them:
// <html class="mobile"><body class="card cardN"><div id="qa">…

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type cssElem struct {
	tag     string
	id      string
	classes []string
	attrs   map[string]string
}

type cssCompound struct {
	tag     string // "" or "*" matches any element
	id      string
	classes []string
	attrs   [][2]string // name, value; value "\x00" means any value
}

type cssSelector struct {
	parts []cssCompound // outermost first
	child []bool        // child[i]: parts[i] and parts[i+1] are joined by ">"
	spec  int           // ids*10000 + (classes+attributes)*100 + types
}

type cssRule struct {
	sel       cssSelector
	hide      bool // display: none
	important bool
}

type styleSheet struct {
	rules []cssRule // in source order
}

var cssCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)

// parseStyleSheet keeps the rules that set display.
func parseStyleSheet(src string) *styleSheet {
	ss := &styleSheet{}
	src = cssCommentRe.ReplaceAllString(src, " ")
	for {
		open := strings.IndexByte(src, '{')
		if open < 0 {
			break
		}
		end, depth := -1, 0
		for i := open; i < len(src) && end < 0; i++ {
			switch src[i] {
			case '{':
				depth++
			case '}':
				if depth--; depth == 0 {
					end = i
				}
			}
		}
		if end < 0 {
			break
		}
		prelude, body := src[:open], src[open+1:end]
		src = src[end+1:]
		// statement at-rules (@import …; @charset …;) and stray braces end up in front
		if i := strings.LastIndexAny(prelude, ";}"); i >= 0 {
			prelude = prelude[i+1:]
		}
		prelude = strings.TrimSpace(prelude)
		if prelude == "" || prelude[0] == '@' {
			continue
		}
		val, important, ok := displayDecl(body)
		if !ok {
			continue
		}
		for _, s := range strings.Split(prelude, ",") {
			if sel, ok := parseSelector(s); ok {
				ss.rules = append(ss.rules, cssRule{sel: sel, hide: val == "none", important: important})
			}
		}
	}
	return ss
}

// displayDecl finds the last display declaration in a block (or a style attribute).
func displayDecl(body string) (val string, important, ok bool) {
	for _, decl := range strings.Split(body, ";") {
		k, v, found := strings.Cut(decl, ":")
		if !found || !strings.EqualFold(strings.TrimSpace(k), "display") {
			continue
		}
		v = strings.ToLower(strings.TrimSpace(v))
		important = strings.HasSuffix(v, "important")
		if i := strings.IndexByte(v, '!'); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		val, ok = v, true
	}
	return val, important, ok
}

func parseSelector(s string) (cssSelector, bool) {
	var sel cssSelector
	child := false
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			i++
			continue
		case c == '>':
			if len(sel.parts) == 0 || child {
				return sel, false
			}
			child = true
			i++
			continue
		}
		comp, n, ok := parseCompound(s[i:])
		if !ok {
			return sel, false
		}
		if len(sel.parts) > 0 {
			sel.child = append(sel.child, child)
		}
		child = false
		sel.parts = append(sel.parts, comp)
		i += n
	}
	if child || len(sel.parts) == 0 {
		return sel, false
	}
	for _, p := range sel.parts {
		if p.id != "" {
			sel.spec += 10000
		}
		sel.spec += 100 * (len(p.classes) + len(p.attrs))
		if p.tag != "" && p.tag != "*" {
			sel.spec++
		}
	}
	return sel, true
}

func isIdentByte(c byte) bool {
	return c == '-' || c == '_' || c >= 0x80 || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// parseCompound reads one compound selector (e.g. div.hint#x[href]) and its length.
func parseCompound(s string) (cssCompound, int, bool) {
	var c cssCompound
	ident := func(i int) (string, int) {
		j := i
		for j < len(s) && isIdentByte(s[j]) {
			j++
		}
		return s[i:j], j
	}
	i := 0
	if i < len(s) && s[i] == '*' {
		c.tag, i = "*", 1
	} else if name, j := ident(0); name != "" {
		c.tag, i = strings.ToLower(name), j
	}
	for i < len(s) {
		switch s[i] {
		case '.', '#':
			name, j := ident(i + 1)
			if name == "" {
				return c, 0, false
			}
			if s[i] == '.' {
				c.classes = append(c.classes, name)
			} else {
				if c.id != "" && c.id != name {
					return c, 0, false
				}
				c.id = name
			}
			i = j
		case '[':
			end := strings.IndexByte(s[i:], ']')
			if end < 0 {
				return c, 0, false
			}
			inner := strings.TrimSpace(s[i+1 : i+end])
			name, val, hasVal := strings.Cut(inner, "=")
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" || strings.ContainsAny(name, "^$*|~ ") {
				return c, 0, false // only [attr] and [attr=value]
			}
			if hasVal {
				val = strings.TrimSpace(val)
				if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
					val = val[1 : len(val)-1]
				}
			} else {
				val = "\x00"
			}
			c.attrs = append(c.attrs, [2]string{name, val})
			i += end + 1
		case ' ', '\t', '\n', '\r', '\f', '>':
			return c, i, i > 0
		default:
			return c, 0, false // pseudo-classes, sibling combinators, escapes
		}
	}
	return c, i, i > 0
}

func (c *cssCompound) matches(e *cssElem) bool {
	if c.tag != "" && c.tag != "*" && c.tag != e.tag {
		return false
	}
	if c.id != "" && c.id != e.id {
		return false
	}
	for _, want := range c.classes {
		found := false
		for _, have := range e.classes {
			if have == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, a := range c.attrs {
		v, ok := e.attrs[a[0]]
		if !ok || a[1] != "\x00" && v != a[1] {
			return false
		}
	}
	return true
}

// matchAt reports whether parts[:pi+1] match with parts[pi] on chain[ei].
func (sel *cssSelector) matchAt(pi int, chain []*cssElem, ei int) bool {
	if !sel.parts[pi].matches(chain[ei]) {
		return false
	}
	if pi == 0 {
		return true
	}
	if sel.child[pi-1] {
		return ei > 0 && sel.matchAt(pi-1, chain, ei-1)
	}
	for j := ei - 1; j >= 0; j-- {
		if sel.matchAt(pi-1, chain, j) {
			return true
		}
	}
	return false
}

// hides reports whether the last element of chain (its ancestors before it)
// ends up with display: none, given its inline style attribute.
func (ss *styleSheet) hides(chain []*cssElem, inline string) bool {
	// cascade order: !important, then inline over the sheet, then specificity, then source order
	type weight struct {
		imp, inline bool
		spec        int
	}
	beats := func(a, b weight) bool {
		if a.imp != b.imp {
			return a.imp
		}
		if a.inline != b.inline {
			return a.inline
		}
		return a.spec >= b.spec // later rules win ties
	}
	var best weight
	hide, set := false, false
	for _, r := range ss.rules {
		w := weight{imp: r.important, spec: r.sel.spec}
		if (!set || beats(w, best)) && r.sel.matchAt(len(r.sel.parts)-1, chain, len(chain)-1) {
			best, hide, set = w, r.hide, true
		}
	}
	if val, imp, ok := displayDecl(inline); ok {
		if w := (weight{imp: imp, inline: true}); !set || beats(w, best) {
			hide = val == "none"
		}
	}
	return hide
}

var (
	styleBlockRe = regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style\s*>`)
	sheetCache   = struct {
		sync.Mutex
		m map[string]*styleSheet
	}{m: map[string]*styleSheet{}}
)

// cardStyleSheet combines the note type's CSS with any <style> blocks in the card.
func cardStyleSheet(css, cardHTML string) *styleSheet {
	var sb strings.Builder
	sb.WriteString(css)
	for _, m := range styleBlockRe.FindAllStringSubmatch(cardHTML, -1) {
		sb.WriteString("\n")
		sb.WriteString(m[1])
	}
	key := sb.String()
	sheetCache.Lock()
	defer sheetCache.Unlock()
	if ss, ok := sheetCache.m[key]; ok {
		return ss
	}
	if len(sheetCache.m) >= 32 {
		clear(sheetCache.m)
	}
	ss := parseStyleSheet(key)
	sheetCache.m[key] = ss
	return ss
}

// hideByCSS removes the elements a stylesheet hides from card HTML, plus
// <button>s: they only work by clicking, and there's nothing to click with.
func hideByCSS(src string, ss *styleSheet, ord int) string {
	chain := []*cssElem{
		{tag: "html", classes: []string{"mobile"}},
		{tag: "body", classes: []string{"card", "card" + strconv.Itoa(ord+1)}},
		{tag: "div", id: "qa"},
	}
	base := len(chain)
	type openTag struct {
		tag    string
		hidden bool // this element or an ancestor is hidden
	}
	var stack []openTag
	hidden := func() bool { return len(stack) > 0 && stack[len(stack)-1].hidden }
	var b strings.Builder
	for len(src) > 0 {
		if lt := strings.IndexByte(src, '<'); lt != 0 {
			text := src
			if lt > 0 {
				text = src[:lt]
			}
			if !hidden() {
				b.WriteString(text)
			}
			src = src[len(text):]
			continue
		}
		if strings.HasPrefix(src, "<!--") {
			end := strings.Index(src, "-->")
			if end < 0 {
				end = len(src)
			} else {
				end += 3
			}
			if !hidden() {
				b.WriteString(src[:end])
			}
			src = src[end:]
			continue
		}
		gt := tagEnd(src)
		if gt < 0 {
			if !hidden() {
				b.WriteString(src)
			}
			break
		}
		tag, raw := src[:gt+1], src[1:gt]
		src = src[gt+1:]
		if raw == "" || raw[0] == '!' || raw[0] == '?' {
			if !hidden() {
				b.WriteString(tag)
			}
			continue
		}
		if raw[0] == '/' {
			// close the matching open tag and anything left open inside it, like the layout does
			name := strings.ToLower(strings.TrimSpace(raw[1:]))
			wasHidden := hidden() // a stray close tag stays wherever it is
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].tag == name {
					wasHidden = stack[i].hidden
					stack, chain = stack[:i], chain[:base+i]
					break
				}
			}
			if !wasHidden {
				b.WriteString(tag)
			}
			continue
		}
		selfClose := strings.HasSuffix(raw, "/")
		name, attrs := parseTag(strings.TrimSuffix(raw, "/"))
		el := &cssElem{tag: name, id: attrs["id"], classes: strings.Fields(attrs["class"]), attrs: attrs}
		hide := hidden() || name == "button" || ss.hides(append(chain, el), attrs["style"])
		if (name == "script" || name == "style") && !selfClose {
			// raw text: copy through to the closing tag untouched
			body := len(src)
			if end := strings.Index(strings.ToLower(src), "</"+name); end >= 0 {
				body = end
				if e := strings.IndexByte(src[end:], '>'); e >= 0 {
					body = end + e + 1
				}
			}
			if !hide {
				b.WriteString(tag)
				b.WriteString(src[:body])
			}
			src = src[body:]
			continue
		}
		if !hide {
			b.WriteString(tag)
		}
		if !voidTags[name] && !selfClose {
			stack = append(stack, openTag{tag: name, hidden: hide})
			chain = append(chain[:base+len(stack)-1], el)
		}
	}
	return b.String()
}
