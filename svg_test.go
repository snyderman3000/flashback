package main

import (
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestSVGBasics(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 30 20" width="300" height="200">
	<defs><clipPath id="c"><rect width="15" height="20"/></clipPath></defs>
	<rect width="30" height="20" fill="#0055a4"/>
	<g clip-path="url(#c)"><circle cx="15" cy="10" r="8" fill="red"/></g>
	<path d="M0 0 L30 20" stroke="#fff" stroke-width="2"/>
	<path fill-rule="evenodd" fill="yellow" d="M20 2h8v8h-8z M22 4h4v4h-4z"/>
	</svg>`
	im, err := RenderSVG([]byte(svg), 150, 150, 3)
	if err != nil {
		t.Fatal(err)
	}
	if im.Bounds().Dx() != 150 || im.Bounds().Dy() != 100 {
		t.Fatalf("size %v", im.Bounds())
	}
	at := func(x, y int) [3]uint8 { o := im.PixOffset(x, y); return [3]uint8{im.Pix[o], im.Pix[o+1], im.Pix[o+2]} }
	if c := at(140, 80); c != [3]uint8{0, 0x55, 0xa4} {
		t.Errorf("background %v", c)
	}
	if c := at(60, 50); c[0] < 200 || c[1] > 60 {
		t.Errorf("clipped circle left half should be red, got %v", c)
	}
	if c := at(90, 30); c[0] > 60 {
		t.Errorf("circle right half should be clipped away, got %v", c)
	}
	if c := at(120, 30); c != [3]uint8{0, 0x55, 0xa4} {
		t.Errorf("evenodd hole should show background, got %v", c)
	}
	if c := at(105, 15); c[0] < 200 || c[1] < 200 || c[2] > 60 {
		t.Errorf("evenodd ring should be yellow, got %v", c)
	}
}

// TestFlagSheet renders real flags from the geography deck to a PNG for eyeballing.
func TestFlagSheet(t *testing.T) {
	dir := os.Getenv("FLAG_DIR")
	out := os.Getenv("FLAG_SHEET")
	if dir == "" || out == "" {
		t.Skip("set FLAG_DIR and FLAG_SHEET")
	}
	names := []string{"united_kingdom", "bosnia_and_herzegovina", "tajikistan", "brazil", "united_states_of_america", "canada", "mexico", "nepal-nobox", "south_korea", "japan", "kenya", "sri_lanka", "bhutan", "england", "seychelles", "south_africa"}
	c := NewCanvas(4*160, 4*110)
	c.Fill(0, 0, c.W, c.H, rgb(0xf3, 0xec, 0xd8))
	for i, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, "ug-flag-"+n+".svg"))
		if err != nil {
			t.Logf("%s: %v", n, err)
			continue
		}
		im, err := RenderSVG(b, 150, 100, 3)
		if err != nil {
			t.Errorf("%s: %v", n, err)
			continue
		}
		c.DrawImage(im, (i%4)*160+5, (i/4)*110+5, im.Bounds().Dx(), im.Bounds().Dy())
	}
	f, _ := os.Create(out)
	png.Encode(f, c)
	f.Close()
}

func TestSVGTiming(t *testing.T) {
	dir := os.Getenv("FLAG_DIR")
	if dir == "" {
		t.Skip("set FLAG_DIR")
	}
	var files []string
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if filepath.Ext(e.Name()) == ".svg" {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	type res struct {
		n string
		d time.Duration
	}
	var worst []res
	var total time.Duration
	for _, f := range files {
		b, _ := os.ReadFile(f)
		t0 := time.Now()
		if _, err := RenderSVG(b, 548, 240, 3); err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
		}
		d := time.Since(t0)
		total += d
		worst = append(worst, res{filepath.Base(f), d})
	}
	sort.Slice(worst, func(i, j int) bool { return worst[i].d > worst[j].d })
	t.Logf("%d svgs, total %v, avg %v", len(files), total, total/time.Duration(max(1, len(files))))
	for _, w := range worst[:min(5, len(worst))] {
		t.Logf("  %s %v", w.n, w.d)
	}
}
