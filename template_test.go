package main

import (
	"strings"
	"testing"
)

func findCard(t *testing.T, p *Package, first string, ord int) int {
	t.Helper()
	for i, c := range p.Cards {
		if c.Ord == ord && strings.HasPrefix(p.Fields(c.Note)[0], first) {
			return i
		}
	}
	t.Fatalf("no card %q ord %d", first, ord)
	return -1
}

func TestTemplates(t *testing.T) {
	for _, name := range []string{"new.apkg", "legacy.apkg"} {
		p := openTest(t, name)
		check := func(ci int, answer bool, want ...string) {
			t.Helper()
			got := p.CardHTML(ci, answer)
			plain := stripHTML(got)
			for _, w := range want {
				neg := strings.HasPrefix(w, "!")
				w = strings.TrimPrefix(w, "!")
				if strings.Contains(plain, w) == neg && strings.Contains(got, w) == neg {
					t.Errorf("%s card %d answer=%v: want %v %q in\n%s", name, ci, answer, !neg, w, got)
				}
			}
		}
		hello := findCard(t, p, "hello", 0)
		check(hello, false, "hello", "!hola")
		check(hello, true, "hello", "hola", `<hr id=answer>`)

		// reversed: card 2 asks the back
		bye2 := findCard(t, p, "goodbye", 1)
		check(bye2, false, "adiós", "!goodbye")

		// cloze 1 and 2 of the mitochondria note
		c1 := findCard(t, p, "The {{c1::mito", 0)
		check(c1, false, "[...]", "!mitochondria", "powerhouse")
		check(c1, true, `<span class="cloze">mitochondria</span>`, "Extra: biology 101")
		c2 := findCard(t, p, "The {{c1::mito", 1)
		check(c2, false, "[energy]", "mitochondria", "!powerhouse")

		// conditionals and FrontSide in a custom type
		perro := findCard(t, p, "perro", 0)
		check(perro, false, "e.g. El perro ladra.", "!(no example)")
		check(perro, true, "dog", "e.g. El perro ladra.")
		gato := findCard(t, p, "gato", 0)
		check(gato, false, "(no example)")

		// image occlusion: card 2 asks the ellipse, other masks stay on
		io := findCard(t, p, "{{c1::image-occlusion", 1)
		got := p.CardHTML(io, false)
		if !strings.Contains(got, `data-io="i|rect|.1,.1,.3,.2;a|ellipse|.5,.4,.15,.2;i|polygon|.2,.7,.4,.95,.05,.95"`) {
			t.Errorf("%s: io front = %s", name, got)
		}
		if strings.Contains(got, "<script") == false {
			t.Errorf("%s: expected template script to still be there (layout hides it)", name)
		}
		back := p.CardHTML(io, true)
		if !strings.Contains(back, "r|ellipse") {
			t.Errorf("%s: io back = %s", name, back)
		}
	}
}

func TestClozeEdgeCases(t *testing.T) {
	cases := []struct {
		in     string
		n      int
		answer bool
		want   string
	}{
		{"{{c1::a}} {{c1::b}} {{c2::c}}", 1, false, `<span class="cloze">[...]</span> <span class="cloze">[...]</span> c`},
		{"{{c1::outer {{c2::inner}} text}}", 2, false, `outer <span class="cloze">[...]</span> text`},
		{"{{c1::outer {{c2::inner}} text}}", 1, true, `<span class="cloze">outer inner text</span>`},
		{"{{c1::a::hint here}}", 1, false, `<span class="cloze">[hint here]</span>`},
		{"x {{c1::y", 1, true, `x <span class="cloze">y</span>`},
		{"no clozes {{ here }}", 1, false, "no clozes {{ here }}"},
	}
	for _, c := range cases {
		if got := renderCloze(c.in, c.n, c.answer); got != c.want {
			t.Errorf("renderCloze(%q,%d,%v) = %q, want %q", c.in, c.n, c.answer, got, c.want)
		}
	}
	if got := furigana(" 日本語[にほんご]を 勉強[べんきょう]", "kana"); got != "にほんごをべんきょう" {
		t.Errorf("kana = %q", got)
	}
}

func TestStripHTML(t *testing.T) {
	got := stripHTML(`<div>pan&nbsp;y<br>mantequilla</div><script>alert(1)</script>[sound:a.mp3] &amp; done`)
	if got != "pan y mantequilla ♪ & done" {
		t.Errorf("got %q", got)
	}
}

func TestFieldEmptiness(t *testing.T) {
	for v, want := range map[string]bool{
		"":                        false,
		" <br> <div></div>&nbsp;": false,
		`<img src="a.png">`:       true,
		"x":                       true,
		"[sound:a.mp3]":           true,
	} {
		if got := nonEmpty(v); got != want {
			t.Errorf("nonEmpty(%q) = %v", v, got)
		}
	}
}
