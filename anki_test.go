package main

import (
	"strings"
	"testing"
)

func openTest(t *testing.T, name string) *Package {
	t.Helper()
	p, err := OpenPackage("testdata/"+name, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestOpenAllFormats(t *testing.T) {
	for _, name := range []string{"new.apkg", "legacy.apkg"} {
		p := openTest(t, name)
		var decks []string
		for _, d := range p.Decks {
			decks = append(decks, d.Name)
		}
		got := strings.Join(decks, "|")
		if got != "Science|Spanish Basics|Spanish Basics::Food|日本語" {
			t.Errorf("%s decks = %q", name, got)
		}
		if len(p.Notes) != 53 {
			t.Errorf("%s notes = %d", name, len(p.Notes))
		}
		// 46 basic + 2 reversed (4 cards) + 2 cloze (3 cards) + 2 vocab + 1 image occlusion (3)
		if len(p.Cards) != 58 {
			t.Errorf("%s cards = %d", name, len(p.Cards))
		}
		var long string
		for i, n := range p.Notes {
			f := p.Fields(i)
			if f[0] == "long one" {
				long = f[1]
			}
			if p.Notetypes[n.Mid] == nil {
				t.Errorf("%s: note %d has unknown note type", name, n.ID)
			}
		}
		if len(long) != 120*69+119 || !strings.HasSuffix(long, "page.") {
			t.Errorf("%s: long field len %d", name, len(long))
		}
		img, err := p.ReadMedia("apple.png")
		if err != nil || !strings.HasPrefix(string(img), "\x89PNG") {
			t.Errorf("%s: media apple.png: %v %q", name, err, img[:min(8, len(img))])
		}
		var vocab *Notetype
		for _, nt := range p.Notetypes {
			if nt.Name == "Vocab+" {
				vocab = nt
			}
			if nt.Name == "Cloze" && !nt.Cloze {
				t.Errorf("%s: cloze type not flagged", name)
			}
		}
		if vocab == nil || strings.Join(vocab.Fields, ",") != "Word,Meaning,Example" || !strings.Contains(vocab.Templates[0].A, "{{FrontSide}}") {
			t.Errorf("%s: vocab note type = %+v", name, vocab)
		}
		if n := len(p.CardsIn("Spanish Basics")); n != 9 {
			t.Errorf("%s: Spanish Basics tree has %d cards, want 9", name, n)
		}
	}
}

func TestGenanki(t *testing.T) {
	p := openTest(t, "genanki.apkg")
	if p.Title != "Country Capitals" || len(p.Cards) != 4 || p.Schema != "collection.anki2" {
		t.Fatalf("got %q %d %s", p.Title, len(p.Cards), p.Schema)
	}
	if f := p.Fields(p.Cards[0].Note); f[1] != "Paris" {
		t.Fatalf("fields %q", f)
	}
}

func TestCacheReuse(t *testing.T) {
	dir := t.TempDir()
	p1, err := OpenPackage("testdata/new.apkg", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	p1.Close()
	p2, err := OpenPackage("testdata/new.apkg", dir, func(s string) { t.Errorf("reconverted: %s", s) })
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Close()
	if len(p2.Cards) != len(p1.Cards) || p2.Fields(0)[0] == "" {
		t.Fatal("cached package differs")
	}
}
