package main

import (
	"os"
	"testing"

	"golang.org/x/image/font/sfnt"
)

func TestRepackOnionCJKFont(t *testing.T) {
	path := os.Getenv("FLASHBACK_CJK_FONT")
	if path == "" {
		path = "../fonts/wqy-microhei.ttc"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skip("CJK font not available here")
	}
	rb, err := repackSFNT(b, 0)
	if err != nil {
		t.Fatal(err)
	}
	f, err := sfnt.Parse(rb)
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	for _, r := range "日本語ねこ한국어" {
		if i, _ := f.GlyphIndex(&buf, r); i == 0 {
			t.Errorf("missing %q", r)
		}
	}
}
