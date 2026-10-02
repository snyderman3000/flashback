package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestRealDeckSweep renders every card of a real shared deck (both sides) through
// the template engine and layout. The Ultimate Geography packages are built by
// testdecks/ug.py from the deck's published CrowdAnki export; they're too big
// to commit, so the test skips when they're absent.
func TestRealDeckSweep(t *testing.T) {
	for _, name := range []string{"ug_new.apkg", "ug_legacy.apkg"} {
		if _, err := os.Stat("testdata/" + name); err != nil {
			t.Skip("real deck not present")
		}
		t0 := time.Now()
		p := openTest(t, name)
		conv := time.Since(t0)
		loadCardFonts("")
		var imgs, missing int
		var empty []string
		t1 := time.Now()
		for ci := range p.Cards {
			for _, ans := range []bool{false, true} {
				h := p.CardHTML(ci, ans)
				L := LayoutHTML(h, 548, 22, 240, 28, 22, p)
				if len(L.Lines) == 0 {
					empty = append(empty, p.Fields(p.Cards[ci].Note)[0])
				}
				for _, ln := range L.Lines {
					for _, it := range ln.items {
						switch {
						case it.kind == itImage:
							imgs++
						case it.kind == itText && strings.HasPrefix(it.text, "[ug-"):
							missing++
						}
					}
				}
			}
		}
		t.Logf("%s: %d cards, convert %v, render both sides %v; images drawn %d, unreadable %d, blank sides %d",
			name, len(p.Cards), conv.Round(time.Millisecond), time.Since(t1).Round(time.Millisecond), imgs, missing, len(empty))
		if missing > 0 {
			t.Errorf("%s: %d images couldn't be read", name, missing)
		}
		if len(empty) > 0 {
			t.Errorf("%s: blank card sides for %v", name, empty[:min(5, len(empty))])
		}
	}
}
