package main

import (
	"strings"
	"testing"
)

// The switches AnKing's note types use, plus a few cascade corner cases.
const testCSS = `
/* TAGS ON/OFF DESKTOP & MOBILE*/
	 #tags-container { display: none; }  /* ‘none’ or ‘block’ */
.clozefield, .mobile .editcloze { display: none; }
.editcloze, .mobile .clozefield { display:  block; }
#pic {display: block; } /* 'none' or 'block' */
.mobile #pic { display: none; }
#extra:hover, #extra::before { display: none; }
@media (max-width: 9999px) { #extra { display: none; } }
@import url(x.css); .card2 .card1-only { display: none }
section > .direct { display: none }
.keep { display: none } .keep { display: block !important } .keep { display: none }
[data-x=gone], p[hidden-ish] { display: none }
.a + .b, .a ~ .b { display: none }
`

func TestHideByCSS(t *testing.T) {
	ss := parseStyleSheet(testCSS)
	for _, tc := range []struct{ in, want string }{
		// the cloze printed twice, one copy hidden
		{`<div class="clozefield">Q <b>red</b></div><div class="editcloze">Q <b>red</b></div>`, "Q red"},
		{`answer<div id="tags-container"><kbd>#AK_Step1</kbd> <kbd>#B&amp;B</kbd></div>`, "answer"},
		{`<div id="pic"><a href="x"><img src="_AnKingRound.png"></a></div>text`, "text"},
		// pseudo-classes and @media blocks never match
		{`<div id="extra">extra</div>`, "extra"},
		{`<div class="card1-only">one</div>`, "one"},
		{`<section><i><span class="direct">kept</span></i><span class="direct">gone</span></section>`, "kept"},
		{`<span class="keep">important wins</span>`, "important wins"},
		{`<span data-x="gone">x</span><p hidden-ish>y</p><span data-x="stay">z</span>`, "z"},
		{`<span class="a">a</span> <span class="b">b</span>`, "a b"},
		// inline style beats the sheet (and still hides on its own)
		{`<div id="tags-container" style="display: block">shown</div><div style="display:none">no</div>`, "shown"},
		// an unclosed tag inside a hidden element doesn't swallow what follows
		{`<div id="tags-container"><span>tag</div>after`, "after"},
		// buttons do nothing on a handheld
		{`<a class="hint" href="#"><button class="button-ln"><img src="i.png"> Lecture Notes</button></a>notes`, "notes"},
		// script bodies pass through untouched
		{`<script>if (a<b && c>d) { x = "<div id='pic'>" }</script>ok`, "ok"},
	} {
		got := hideByCSS(tc.in, ss, 0)
		if plain := stripHTML(got); plain != tc.want {
			t.Errorf("hideByCSS(%q)\n = %q\nplain %q, want %q", tc.in, got, plain, tc.want)
		}
	}
}

func TestCardStyleBlocks(t *testing.T) {
	// AnKing's answer template keeps its Wikipedia popup hidden with a <style> in the template
	h := `back<div id="popup-container"><button>&times;</button><a id="open-wiki-btn" href="">&#8618;</a></div>
<style>#popup-container { display: none; } #tc::-webkit-scrollbar { display: none; }</style>`
	if got := stripHTML(hideByCSS(h, cardStyleSheet("", h), 0)); got != "back" {
		t.Errorf("got %q", got)
	}
}

func TestStyledTemplate(t *testing.T) {
	nt := &Notetype{Name: "Cloze-AnKing", Cloze: true, Fields: []string{"Text", "Extra"},
		CSS:       testCSS,
		Templates: []Template{{Q: "{{cloze:Text}}{{#Tags}}<div id=tags-container>{{clickable::Tags}}</div>{{/Tags}}", A: `<div class="clozefield">{{cloze:Text}}</div><div class="editcloze">{{edit:cloze:Text}}</div><hr>{{Extra}}{{#Tags}}<div id="tags-container">{{clickable::Tags}}</div>{{/Tags}}`}}}
	x := &tmplCtx{p: &Package{}, nt: nt, flds: []string{"Favourite colour: {{c1::red}}", "extra"}, tags: "#AK_Step1_v12::colours"}
	q := x.styled(x.render(nt.Templates[0].Q, false, ""))
	a := x.styled(x.render(nt.Templates[0].A, true, q))
	if got := stripHTML(q); got != "Favourite colour: [...]" {
		t.Errorf("question %q", got)
	}
	if got := stripHTML(a); got != "Favourite colour: red extra" {
		t.Errorf("answer %q", got)
	}
}

func TestNotetypeCSSRead(t *testing.T) {
	for _, name := range []string{"new.apkg", "legacy.apkg"} {
		p := openTest(t, name)
		n := 0
		for _, nt := range p.Notetypes {
			if strings.Contains(nt.CSS, ".card") {
				n++
			}
		}
		if n == 0 {
			t.Errorf("%s: no note type CSS read", name)
		}
	}
}
