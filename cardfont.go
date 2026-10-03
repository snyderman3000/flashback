package main

// Card fonts: a typewriter face (Courier Prime) in four styles, with fallback
// fonts for anything it lacks — DejaVu Sans (shipped) for Greek, Cyrillic, math
// and symbols, then the CJK font OnionOS ships in miyoo/app, then any fonts the
// user drops in App/Flashback/fonts.

import (
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

//go:embed assets/CourierPrime-Regular.ttf
var cpRegular []byte

//go:embed assets/CourierPrime-Bold.ttf
var cpBold []byte

//go:embed assets/CourierPrime-Italic.ttf
var cpItalic []byte

//go:embed assets/CourierPrime-BoldItalic.ttf
var cpBoldItalic []byte

type fontSrc struct {
	name string
	f    *sfnt.Font
	buf  sfnt.Buffer
}

func (s *fontSrc) has(r rune) bool {
	i, err := s.f.GlyphIndex(&s.buf, r)
	return err == nil && i != 0
}

var (
	cardSrcOnce sync.Once
	cardStyles  [4]*fontSrc // regular, bold, italic, bold italic
	cardFalls   []*fontSrc
)

// systemFontDirs are searched for fallback fonts (in order).
var systemFontPaths = []string{
	"/mnt/SDCARD/miyoo/app/wqy-microhei.ttc", // OnionOS: Chinese, Japanese, Korean
	"/mnt/SDCARD/miyoo/app/MicrosoftYaHeiGB.ttf",
	"/mnt/SDCARD/.system/res/font1.ttf", // NextUI / NX Redux: Rounded M+ (Japanese)
}

func loadCardFonts(appDir string) {
	cardSrcOnce.Do(func() {
		for i, b := range [][]byte{cpRegular, cpBold, cpItalic, cpBoldItalic} {
			f, err := sfnt.Parse(b)
			if err != nil {
				panic(err)
			}
			cardStyles[i] = &fontSrc{name: "Courier Prime", f: f}
		}
		var paths []string
		if appDir != "" {
			user, _ := filepath.Glob(filepath.Join(appDir, "fonts", "*"))
			sort.Strings(user)
			paths = append(paths, user...)
			paths = append(paths, filepath.Join(appDir, "DejaVuSans.ttf"))
		}
		if d := os.Getenv("FLASHBACK_FONTS"); d != "" { // for desktop testing
			for _, p := range strings.Split(d, ":") {
				paths = append(paths, p)
			}
		}
		paths = append(paths, systemFontPaths...)
		seen := map[string]bool{}
		for _, p := range paths {
			if seen[p] {
				continue
			}
			seen[p] = true
			ext := strings.ToLower(filepath.Ext(p))
			if ext != ".ttf" && ext != ".otf" && ext != ".ttc" && ext != ".otc" {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			var f *sfnt.Font
			if ext == ".ttc" || ext == ".otc" {
				var c *sfnt.Collection
				if c, err = sfnt.ParseCollection(b); err == nil {
					f, err = c.Font(0)
				}
			} else {
				f, err = sfnt.Parse(b)
			}
			if err != nil {
				// unaligned tables are common in older fonts; repack and retry
				if rb, rerr := repackSFNT(b, 0); rerr == nil {
					f, err = sfnt.Parse(rb)
				}
			}
			if err != nil {
				log.Printf("font %s: %v", p, err)
				continue
			}
			cardFalls = append(cardFalls, &fontSrc{name: filepath.Base(p), f: f})
		}
	})
}

// FontSet draws text in one style and size, falling back per character.
type FontSet struct {
	size   float64
	style  int
	faces  map[*fontSrc]font.Face
	cache  map[rune]*cglyph
	ascent int
	height int
}

type cglyph struct {
	mask    *image.Alpha
	off     image.Point
	advance int
	fake    bool // embolden by double-strike (fallback font has no bold)
}

var fontSetCache = map[[2]int]*FontSet{}

// cardFont returns the font set for a pixel size and style (0 regular, 1 bold, 2 italic, 3 bold italic).
func cardFont(size, style int) *FontSet {
	k := [2]int{size, style}
	if fs := fontSetCache[k]; fs != nil {
		return fs
	}
	loadCardFonts("")
	fs := &FontSet{size: float64(size), style: style, faces: map[*fontSrc]font.Face{}, cache: map[rune]*cglyph{}}
	m := fs.face(cardStyles[style]).Metrics()
	fs.ascent = m.Ascent.Ceil()
	fs.height = m.Height.Ceil()
	fontSetCache[k] = fs
	return fs
}

func (fs *FontSet) face(src *fontSrc) font.Face {
	if f, ok := fs.faces[src]; ok {
		return f
	}
	f, err := opentype.NewFace(src.f, &opentype.FaceOptions{Size: fs.size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		f = nil
	}
	fs.faces[src] = f
	return f
}

func (fs *FontSet) glyph(r rune) *cglyph {
	if g, ok := fs.cache[r]; ok {
		return g
	}
	if r == ' ' {
		r = ' '
	}
	srcs := append([]*fontSrc{cardStyles[fs.style]}, cardFalls...)
	if fs.style != 0 {
		srcs = append([]*fontSrc{cardStyles[fs.style], cardStyles[0]}, cardFalls...)
	}
	var g *cglyph
	for i, s := range srcs {
		if r != ' ' && !s.has(r) {
			continue
		}
		face := fs.face(s)
		if face == nil {
			continue
		}
		dr, mask, mp, adv, ok := face.Glyph(fixed.P(0, 0), r)
		if !ok {
			continue
		}
		a := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
		draw.Draw(a, a.Bounds(), mask, mp, draw.Src)
		g = &cglyph{mask: a, off: dr.Min, advance: adv.Round()}
		if fs.style&1 == 1 && s != cardStyles[fs.style] && (i > 0) {
			g.fake = true
			g.advance++
		}
		break
	}
	if g == nil {
		// draw a hollow box for characters no font has
		w := fs.ascent * 6 / 10
		a := image.NewAlpha(image.Rect(0, 0, w, fs.ascent))
		for y := 0; y < fs.ascent; y++ {
			for x := 0; x < w; x++ {
				if x == 0 || y == 0 || x == w-1 || y == fs.ascent-1 {
					a.Pix[y*a.Stride+x] = 140
				}
			}
		}
		g = &cglyph{mask: a, off: image.Pt(1, -fs.ascent), advance: w + 3}
	}
	fs.cache[r] = g
	return g
}

func (fs *FontSet) Width(s string) int {
	w := 0
	for _, r := range s {
		w += fs.glyph(r).advance
	}
	return w
}

// TextSet draws s with its top-left at (x,y) and returns the x after it.
func (c *Canvas) TextSet(fs *FontSet, x, y int, s string, col color.RGBA) int {
	base := y + fs.ascent
	for _, r := range s {
		g := fs.glyph(r)
		if g.mask != nil {
			c.drawMask(g.mask, x+g.off.X, base+g.off.Y, col)
			if g.fake {
				c.drawMask(g.mask, x+g.off.X+1, base+g.off.Y, col)
			}
		}
		x += g.advance
	}
	return x
}

func (c *Canvas) drawMask(m *image.Alpha, gx, gy int, col color.RGBA) {
	b := m.Bounds()
	for yy := 0; yy < b.Dy(); yy++ {
		py := gy + yy
		if py < 0 || py >= c.H {
			continue
		}
		for xx := 0; xx < b.Dx(); xx++ {
			px := gx + xx
			if px < 0 || px >= c.W {
				continue
			}
			a := int(m.Pix[yy*m.Stride+xx])
			if a == 0 {
				continue
			}
			o := py*c.Stride + px*4
			c.Pix[o] = uint8((int(c.Pix[o])*(255-a) + int(col.R)*a) / 255)
			c.Pix[o+1] = uint8((int(c.Pix[o+1])*(255-a) + int(col.G)*a) / 255)
			c.Pix[o+2] = uint8((int(c.Pix[o+2])*(255-a) + int(col.B)*a) / 255)
		}
	}
}

// Ellipsize trims s to fit width w.
func (fs *FontSet) Ellipsize(s string, w int) string {
	if fs.Width(s) <= w {
		return s
	}
	rs := []rune(s)
	for len(rs) > 0 {
		rs = rs[:len(rs)-1]
		if fs.Width(string(rs)+"…") <= w {
			return strings.TrimRight(string(rs), " ") + "…"
		}
	}
	return ""
}

// Wrap breaks plain text into lines no wider than w.
func (fs *FontSet) Wrap(s string, w int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, wd := range strings.Fields(para) {
			try := wd
			if line != "" {
				try = line + " " + wd
			}
			if fs.Width(try) <= w || line == "" {
				line = try
				continue
			}
			out = append(out, line)
			line = wd
		}
		out = append(out, line)
	}
	return out
}
