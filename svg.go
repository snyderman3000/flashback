package main

// A compact SVG rasterizer for card images (flags, maps, diagrams). It covers
// what shared decks actually use: path (all commands incl. arcs), rect, circle,
// ellipse, line, polyline, polygon, g, use/defs, transforms, clip paths, fills
// and strokes with nonzero/evenodd rules, opacity, and gradients (drawn as
// their average colour). Text, filters, masks and patterns are skipped.

import (
	"bytes"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
)

type svgNode struct {
	name     string
	attrs    map[string]string
	children []*svgNode
}

type affine [6]float64 // a b c d e f: x' = a*x + c*y + e, y' = b*x + d*y + f

var identity = affine{1, 0, 0, 1, 0, 0}

func (m affine) mul(n affine) affine { // m then n applied first: m·n
	return affine{
		m[0]*n[0] + m[2]*n[1], m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3], m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4], m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

func (m affine) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

func (m affine) scale() float64 { return math.Sqrt(math.Abs(m[0]*m[3] - m[1]*m[2])) }

type pt struct{ x, y float64 }

func parseSVG(b []byte) (*svgNode, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	var stack []*svgNode
	var root *svgNode
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &svgNode{name: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				k := a.Name.Local
				if a.Name.Space != "" && k == "href" {
					k = "href"
				}
				n.attrs[k] = a.Value
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.children = append(p.children, n)
			} else if n.name == "svg" {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if root == nil {
		return nil, errors.New("no <svg> element")
	}
	return root, nil
}

// svgSize returns the intrinsic size and the viewBox.
func svgSize(root *svgNode) (w, h float64, vb [4]float64) {
	if v := root.attrs["viewBox"]; v != "" {
		f := svgNums(v)
		if len(f) == 4 && f[2] > 0 && f[3] > 0 {
			vb = [4]float64{f[0], f[1], f[2], f[3]}
		}
	}
	w, _ = svgLength(root.attrs["width"])
	h, _ = svgLength(root.attrs["height"])
	if vb[2] == 0 {
		if w == 0 {
			w = 300
		}
		if h == 0 {
			h = 150
		}
		vb = [4]float64{0, 0, w, h}
	}
	if w == 0 && h == 0 {
		w, h = vb[2], vb[3]
	} else if w == 0 {
		w = h * vb[2] / vb[3]
	} else if h == 0 {
		h = w * vb[3] / vb[2]
	}
	return
}

func svgLength(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, "%") {
		return 0, false
	}
	mult := 1.0
	for _, u := range []struct {
		suf string
		m   float64
	}{{"px", 1}, {"pt", 4.0 / 3}, {"pc", 16}, {"mm", 3.7795}, {"cm", 37.795}, {"in", 96}, {"em", 16}} {
		if strings.HasSuffix(s, u.suf) {
			s, mult = strings.TrimSuffix(s, u.suf), u.m
			break
		}
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f * mult, err == nil
}

// svgNums splits a list of numbers like "1,2 3-4.5e2.5" correctly.
func svgNums(s string) []float64 {
	var out []float64
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == ',' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		j := i
		if s[j] == '+' || s[j] == '-' {
			j++
		}
		dot, exp := false, false
		for j < len(s) {
			c := s[j]
			switch {
			case c >= '0' && c <= '9':
			case c == '.' && !dot && !exp:
				dot = true
			case (c == 'e' || c == 'E') && !exp:
				exp = true
				if j+1 < len(s) && (s[j+1] == '-' || s[j+1] == '+') {
					j++
				}
			default:
				goto done
			}
			j++
		}
	done:
		if j == i {
			i++
			continue
		}
		f, err := strconv.ParseFloat(s[i:j], 64)
		if err == nil {
			out = append(out, f)
		}
		i = j
	}
	return out
}

func parseTransform(s string) affine {
	m := identity
	for s = strings.TrimSpace(s); s != ""; s = strings.TrimSpace(s) {
		open := strings.IndexByte(s, '(')
		close := strings.IndexByte(s, ')')
		if open < 0 || close < open {
			break
		}
		name := strings.TrimSpace(strings.Trim(s[:open], ", "))
		a := svgNums(s[open+1 : close])
		s = s[close+1:]
		var t affine
		switch name {
		case "matrix":
			if len(a) < 6 {
				continue
			}
			t = affine{a[0], a[1], a[2], a[3], a[4], a[5]}
		case "translate":
			if len(a) == 0 {
				continue
			}
			ty := 0.0
			if len(a) > 1 {
				ty = a[1]
			}
			t = affine{1, 0, 0, 1, a[0], ty}
		case "scale":
			if len(a) == 0 {
				continue
			}
			sy := a[0]
			if len(a) > 1 {
				sy = a[1]
			}
			t = affine{a[0], 0, 0, sy, 0, 0}
		case "rotate":
			if len(a) == 0 {
				continue
			}
			r := a[0] * math.Pi / 180
			cs, sn := math.Cos(r), math.Sin(r)
			t = affine{cs, sn, -sn, cs, 0, 0}
			if len(a) >= 3 {
				t = affine{1, 0, 0, 1, a[1], a[2]}.mul(t).mul(affine{1, 0, 0, 1, -a[1], -a[2]})
			}
		case "skewX":
			if len(a) == 0 {
				continue
			}
			t = affine{1, 0, math.Tan(a[0] * math.Pi / 180), 1, 0, 0}
		case "skewY":
			if len(a) == 0 {
				continue
			}
			t = affine{1, math.Tan(a[0] * math.Pi / 180), 0, 1, 0, 0}
		default:
			continue
		}
		m = m.mul(t)
	}
	return m
}

// ---- styles ----

type svgPaint struct {
	set  bool
	none bool
	col  color.RGBA
	ref  string // url(#id)
}

type svgStyle struct {
	fill, stroke         svgPaint
	fillOp, strokeOp, op float64
	strokeW              float64
	evenOdd              bool
	color                color.RGBA
	clip                 string
	display              bool
}

func defaultSVGStyle() svgStyle {
	return svgStyle{fill: svgPaint{set: true, col: rgb(0, 0, 0)}, stroke: svgPaint{none: true}, fillOp: 1, strokeOp: 1, op: 1, strokeW: 1, color: rgb(0, 0, 0), display: true}
}

func (n *svgNode) prop(k string) (string, bool) {
	if st := n.attrs["style"]; st != "" {
		for _, decl := range strings.Split(st, ";") {
			kk, v, ok := strings.Cut(decl, ":")
			if ok && strings.TrimSpace(kk) == k {
				return strings.TrimSpace(v), true
			}
		}
	}
	v, ok := n.attrs[k]
	return strings.TrimSpace(v), ok
}

func parsePaint(v string, cur color.RGBA) svgPaint {
	v = strings.TrimSpace(v)
	switch {
	case v == "none" || v == "transparent":
		return svgPaint{set: true, none: true}
	case v == "currentColor":
		return svgPaint{set: true, col: cur}
	case strings.HasPrefix(v, "url("):
		end := strings.IndexByte(v, ')')
		ref := ""
		if end > 4 {
			ref = strings.Trim(strings.TrimSpace(v[4:end]), `'"#`)
		}
		return svgPaint{set: true, ref: ref}
	}
	if c, ok := parseColor(v); ok {
		return svgPaint{set: true, col: c}
	}
	return svgPaint{}
}

func (st svgStyle) inherit(n *svgNode) svgStyle {
	if v, ok := n.prop("color"); ok {
		if c, ok := parseColor(v); ok {
			st.color = c
		}
	}
	if v, ok := n.prop("fill"); ok {
		if p := parsePaint(v, st.color); p.set {
			st.fill = p
		}
	}
	if v, ok := n.prop("stroke"); ok {
		if p := parsePaint(v, st.color); p.set {
			st.stroke = p
		}
	}
	num := func(k string, dst *float64) {
		if v, ok := n.prop(k); ok {
			if f, ok := svgLength(v); ok {
				*dst = f
			} else if strings.HasSuffix(v, "%") {
				if f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64); err == nil {
					*dst = f / 100
				}
			}
		}
	}
	num("fill-opacity", &st.fillOp)
	num("stroke-opacity", &st.strokeOp)
	num("stroke-width", &st.strokeW)
	op := 1.0
	num("opacity", &op)
	st.op *= op // (group opacity applied per shape: close enough for flags)
	if v, ok := n.prop("fill-rule"); ok {
		st.evenOdd = v == "evenodd"
	}
	if v, ok := n.prop("display"); ok && v == "none" {
		st.display = false
	}
	if v, ok := n.prop("visibility"); ok && v == "hidden" {
		st.display = false
	}
	return st
}

// ---- renderer ----

type svgRenderer struct {
	vw, vh float64 // viewBox size, for percentage lengths
	dst    *image.RGBA
	ids    map[string]*svgNode
	depth  int
}

// RenderSVG rasterizes an SVG to fit within maxW×maxH, on white.
func RenderSVG(b []byte, maxW, maxH int, upscale float64) (*image.RGBA, error) {
	root, err := parseSVG(b)
	if err != nil {
		return nil, err
	}
	w, h, vb := svgSize(root)
	if w <= 0 || h <= 0 {
		return nil, errors.New("svg has no size")
	}
	scale := math.Min(float64(maxW)/w, float64(maxH)/h)
	scale = math.Min(scale, upscale)
	pw, ph := max(1, int(math.Round(w*scale))), max(1, int(math.Round(h*scale)))
	dst := image.NewRGBA(image.Rect(0, 0, pw, ph))
	for i := 0; i < len(dst.Pix); i++ {
		dst.Pix[i] = 255
	}
	// viewBox → pixels (xMidYMid meet)
	s := math.Min(float64(pw)/vb[2], float64(ph)/vb[3])
	ox := (float64(pw) - vb[2]*s) / 2
	oy := (float64(ph) - vb[3]*s) / 2
	m := affine{s, 0, 0, s, ox - vb[0]*s, oy - vb[1]*s}
	r := &svgRenderer{dst: dst, ids: map[string]*svgNode{}, vw: vb[2], vh: vb[3]}
	r.index(root)
	r.draw(root, m, defaultSVGStyle(), nil)
	return dst, nil
}

func (r *svgRenderer) index(n *svgNode) {
	if id := n.attrs["id"]; id != "" {
		r.ids[id] = n
	}
	for _, c := range n.children {
		r.index(c)
	}
}

func (r *svgRenderer) draw(n *svgNode, m affine, st svgStyle, clip []float32) {
	r.depth++
	defer func() { r.depth-- }()
	if r.depth > 40 {
		return
	}
	switch n.name {
	case "defs", "clipPath", "mask", "linearGradient", "radialGradient", "pattern", "symbol", "title", "desc",
		"metadata", "style", "script", "filter", "text", "marker", "font", "foreignObject":
		return
	}
	st = st.inherit(n)
	if !st.display {
		return
	}
	if t, ok := n.attrs["transform"]; ok {
		m = m.mul(parseTransform(t))
	}
	if cp, ok := n.prop("clip-path"); ok && strings.HasPrefix(cp, "url(") {
		end := strings.IndexByte(cp, ')')
		if end > 4 {
			if cn := r.ids[strings.Trim(cp[4:end], `'"# `)]; cn != nil {
				cm := m
				if cn.attrs["clipPathUnits"] == "objectBoundingBox" {
					if bx, by, bw, bh, ok := r.bbox(n, identity, 0); ok {
						cm = m.mul(affine{bw, 0, 0, bh, bx, by})
					}
				}
				clip = r.clipMask(cn, cm, clip)
			}
		}
	}
	switch n.name {
	case "svg", "g", "a", "switch":
		if n.name == "svg" && r.depth > 1 {
			// nested <svg>: position by x/y
			x, _ := svgLength(n.attrs["x"])
			y, _ := svgLength(n.attrs["y"])
			m = m.mul(affine{1, 0, 0, 1, x, y})
		}
		for _, c := range n.children {
			r.draw(c, m, st, clip)
		}
	case "use":
		ref := strings.TrimPrefix(n.attrs["href"], "#")
		if target := r.ids[ref]; target != nil && target != n {
			x, _ := svgLength(n.attrs["x"])
			y, _ := svgLength(n.attrs["y"])
			mm := m.mul(affine{1, 0, 0, 1, x, y})
			if target.name == "symbol" {
				for _, c := range target.children {
					r.draw(c, mm, st, clip)
				}
			} else {
				r.draw(target, mm, st, clip)
			}
		}
	default:
		subs := r.shapePath(n)
		if len(subs) == 0 {
			return
		}
		r.paint(subs, m, st, clip)
	}
}

func (r *svgRenderer) paintColor(p svgPaint) (color.RGBA, bool) {
	if p.none || !p.set {
		return color.RGBA{}, false
	}
	if p.ref == "" {
		return p.col, true
	}
	g := r.ids[p.ref]
	for i := 0; g != nil && i < 4; i++ {
		var stops []*svgNode
		for _, c := range g.children {
			if c.name == "stop" {
				stops = append(stops, c)
			}
		}
		if len(stops) > 0 {
			var rr, gg, bb, n float64
			for _, s := range stops {
				v, _ := s.prop("stop-color")
				c, ok := parseColor(v)
				if !ok {
					c = rgb(0, 0, 0)
				}
				rr, gg, bb, n = rr+float64(c.R), gg+float64(c.G), bb+float64(c.B), n+1
			}
			return rgb(uint8(rr/n), uint8(gg/n), uint8(bb/n)), true
		}
		g = r.ids[strings.TrimPrefix(g.attrs["href"], "#")]
	}
	return rgb(0x80, 0x80, 0x80), true
}

func (r *svgRenderer) paint(subs [][]pt, m affine, st svgStyle, clip []float32) {
	tsubs := make([][]pt, len(subs))
	for i, s := range subs {
		t := make([]pt, len(s))
		for j, p := range s {
			t[j].x, t[j].y = m.apply(p.x, p.y)
		}
		tsubs[i] = t
	}
	if c, ok := r.paintColor(st.fill); ok {
		fillPolys(r.dst, tsubs, st.evenOdd, c, st.fillOp*st.op, clip)
	}
	if c, ok := r.paintColor(st.stroke); ok && st.strokeW > 0 {
		w := st.strokeW * m.scale()
		if w < 0.6 {
			w = 0.6 // keep hairlines visible
		}
		fillPolys(r.dst, strokePolys(tsubs, w, subsClosed(subs)), false, c, st.strokeOp*st.op, clip)
	}
}

// subsClosed reports which subpaths were closed (first point repeated at the end).
func subsClosed(subs [][]pt) []bool {
	out := make([]bool, len(subs))
	for i, s := range subs {
		out[i] = len(s) > 2 && s[0] == s[len(s)-1]
	}
	return out
}

func (r *svgRenderer) clipMask(cn *svgNode, m affine, parent []float32) []float32 {
	b := r.dst.Bounds()
	mask := make([]float32, b.Dx()*b.Dy())
	cm := m
	if t, ok := cn.attrs["transform"]; ok {
		cm = cm.mul(parseTransform(t))
	}
	var add func(n *svgNode, mm affine)
	add = func(n *svgNode, mm affine) {
		if t, ok := n.attrs["transform"]; ok && n != cn {
			mm = mm.mul(parseTransform(t))
		}
		if n.name == "use" {
			if t := r.ids[strings.TrimPrefix(n.attrs["href"], "#")]; t != nil && t != n {
				x, _ := svgLength(n.attrs["x"])
				y, _ := svgLength(n.attrs["y"])
				add(t, mm.mul(affine{1, 0, 0, 1, x, y}))
			}
			return
		}
		if subs := r.shapePath(n); len(subs) > 0 {
			t := make([][]pt, len(subs))
			for i, s := range subs {
				t[i] = make([]pt, len(s))
				for j, p := range s {
					t[i][j].x, t[i][j].y = mm.apply(p.x, p.y)
				}
			}
			cv := coverage(t, b.Dx(), b.Dy(), false)
			for yy := 0; yy < cv.h; yy++ {
				for xx := 0; xx < cv.w; xx++ {
					i := (cv.y0+yy)*b.Dx() + cv.x0 + xx
					mask[i] = max(mask[i], cv.v[yy*cv.w+xx])
				}
			}
		}
		for _, c := range n.children {
			add(c, mm)
		}
	}
	add(cn, cm)
	if parent != nil {
		for i := range mask {
			mask[i] *= parent[i]
		}
	}
	return mask
}

// bbox returns the bounding box of an element's shapes in its own user space.
func (r *svgRenderer) bbox(n *svgNode, m affine, depth int) (x, y, w, h float64, ok bool) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	var walk func(n *svgNode, m affine, d int)
	walk = func(n *svgNode, m affine, d int) {
		if d > 20 {
			return
		}
		for _, s := range r.shapePath(n) {
			for _, p := range s {
				px, py := m.apply(p.x, p.y)
				minX, minY, maxX, maxY = math.Min(minX, px), math.Min(minY, py), math.Max(maxX, px), math.Max(maxY, py)
			}
		}
		if n.name == "use" {
			if t := r.ids[strings.TrimPrefix(n.attrs["href"], "#")]; t != nil && t != n {
				ux, _ := svgLength(n.attrs["x"])
				uy, _ := svgLength(n.attrs["y"])
				mm := m.mul(affine{1, 0, 0, 1, ux, uy})
				if tr, ok := t.attrs["transform"]; ok {
					mm = mm.mul(parseTransform(tr))
				}
				walk(t, mm, d+1)
			}
		}
		for _, c := range n.children {
			mm := m
			if tr, ok := c.attrs["transform"]; ok {
				mm = mm.mul(parseTransform(tr))
			}
			walk(c, mm, d+1)
		}
	}
	walk(n, m, depth)
	if maxX < minX || maxY < minY {
		return 0, 0, 0, 0, false
	}
	return minX, minY, maxX - minX, maxY - minY, true
}

// ---- geometry ----

// shapePath converts a basic shape element into flattened subpaths (user space).
func (r *svgRenderer) shapePath(n *svgNode) [][]pt {
	f := func(k string) float64 {
		v := strings.TrimSpace(n.attrs[k])
		if strings.HasSuffix(v, "%") {
			p, _ := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
			switch k {
			case "x", "width", "cx", "rx", "x1", "x2":
				return p / 100 * r.vw
			case "y", "height", "cy", "ry", "y1", "y2":
				return p / 100 * r.vh
			}
			return p / 100 * math.Sqrt((r.vw*r.vw+r.vh*r.vh)/2)
		}
		l, _ := svgLength(v)
		return l
	}
	switch n.name {
	case "path":
		return flattenPath(n.attrs["d"])
	case "rect":
		x, y, w, h := f("x"), f("y"), f("width"), f("height")
		if w <= 0 || h <= 0 {
			return nil
		}
		rx, ry := f("rx"), f("ry")
		if rx == 0 {
			rx = ry
		}
		if ry == 0 {
			ry = rx
		}
		if rx <= 0 {
			return [][]pt{{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}, {x, y}}}
		}
		rx, ry = math.Min(rx, w/2), math.Min(ry, h/2)
		var s []pt
		corner := func(cx, cy, a0 float64) {
			for i := 0; i <= 6; i++ {
				a := a0 + float64(i)*math.Pi/12
				s = append(s, pt{cx + rx*math.Cos(a), cy + ry*math.Sin(a)})
			}
		}
		corner(x+w-rx, y+ry, -math.Pi/2)
		corner(x+w-rx, y+h-ry, 0)
		corner(x+rx, y+h-ry, math.Pi/2)
		corner(x+rx, y+ry, math.Pi)
		s = append(s, s[0])
		return [][]pt{s}
	case "circle":
		r := f("r")
		return ellipsePts(f("cx"), f("cy"), r, r)
	case "ellipse":
		return ellipsePts(f("cx"), f("cy"), f("rx"), f("ry"))
	case "line":
		return [][]pt{{{f("x1"), f("y1")}, {f("x2"), f("y2")}}}
	case "polyline", "polygon":
		nums := svgNums(n.attrs["points"])
		var s []pt
		for i := 0; i+1 < len(nums); i += 2 {
			s = append(s, pt{nums[i], nums[i+1]})
		}
		if n.name == "polygon" && len(s) > 2 {
			s = append(s, s[0])
		}
		if len(s) < 2 {
			return nil
		}
		return [][]pt{s}
	}
	return nil
}

func ellipsePts(cx, cy, rx, ry float64) [][]pt {
	if rx <= 0 || ry <= 0 {
		return nil
	}
	n := 48
	s := make([]pt, 0, n+1)
	for i := 0; i <= n; i++ {
		a := float64(i) * 2 * math.Pi / float64(n)
		s = append(s, pt{cx + rx*math.Cos(a), cy + ry*math.Sin(a)})
	}
	return [][]pt{s}
}

// flattenPath parses SVG path data into polylines.
func flattenPath(d string) [][]pt {
	var subs [][]pt
	var cur []pt
	var x, y, sx, sy float64
	var lcx, lcy float64 // last control point for S/T
	var prev byte
	flush := func() {
		if len(cur) > 1 {
			subs = append(subs, cur)
		}
		cur = nil
	}
	lineTo := func(nx, ny float64) {
		if len(cur) == 0 {
			cur = append(cur, pt{x, y})
		}
		cur = append(cur, pt{nx, ny})
		x, y = nx, ny
	}
	cubic := func(x1, y1, x2, y2, x3, y3 float64) {
		n := 12
		x0, y0 := x, y
		for i := 1; i <= n; i++ {
			t := float64(i) / float64(n)
			u := 1 - t
			px := u*u*u*x0 + 3*u*u*t*x1 + 3*u*t*t*x2 + t*t*t*x3
			py := u*u*u*y0 + 3*u*u*t*y1 + 3*u*t*t*y2 + t*t*t*y3
			lineTo(px, py)
		}
		lcx, lcy = x2, y2
	}
	quad := func(x1, y1, x2, y2 float64) {
		n := 10
		x0, y0 := x, y
		for i := 1; i <= n; i++ {
			t := float64(i) / float64(n)
			u := 1 - t
			lineTo(u*u*x0+2*u*t*x1+t*t*x2, u*u*y0+2*u*t*y1+t*t*y2)
		}
		lcx, lcy = x1, y1
	}
	i := 0
	var cmd byte
	for i < len(d) {
		c := d[i]
		if (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') && c != 'e' && c != 'E' {
			cmd = c
			i++
		} else if cmd == 0 {
			i++
			continue
		}
		// gather the numbers for this command
		j := i
		for j < len(d) {
			c := d[j]
			if (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') && c != 'e' && c != 'E' {
				break
			}
			j++
		}
		var args []float64
		if cmd == 'a' || cmd == 'A' {
			args = arcNums(d[i:j])
		} else {
			args = svgNums(d[i:j])
		}
		i = j
		rel := cmd >= 'a'
		up := cmd &^ 0x20
		need := map[byte]int{'M': 2, 'L': 2, 'H': 1, 'V': 1, 'C': 6, 'S': 4, 'Q': 4, 'T': 2, 'A': 7, 'Z': 0}[up]
		if up == 'Z' {
			if len(cur) > 0 {
				lineTo(sx, sy)
			}
			flush()
			x, y = sx, sy
			prev = 'Z'
			continue
		}
		if need == 0 || len(args) < need {
			continue
		}
		for k := 0; k+need <= len(args); k += need {
			a := args[k : k+need]
			ox, oy := 0.0, 0.0
			if rel {
				ox, oy = x, y
			}
			switch up {
			case 'M':
				if k == 0 {
					flush()
					x, y = a[0]+ox, a[1]+oy
					sx, sy = x, y
					cur = []pt{{x, y}}
				} else {
					lineTo(a[0]+ox, a[1]+oy)
				}
			case 'L':
				lineTo(a[0]+ox, a[1]+oy)
			case 'H':
				lineTo(a[0]+ox, y)
			case 'V':
				lineTo(x, a[0]+oy)
			case 'C':
				cubic(a[0]+ox, a[1]+oy, a[2]+ox, a[3]+oy, a[4]+ox, a[5]+oy)
			case 'S':
				x1, y1 := x, y
				if prev == 'C' || prev == 'S' {
					x1, y1 = 2*x-lcx, 2*y-lcy
				}
				cubic(x1, y1, a[0]+ox, a[1]+oy, a[2]+ox, a[3]+oy)
			case 'Q':
				quad(a[0]+ox, a[1]+oy, a[2]+ox, a[3]+oy)
			case 'T':
				x1, y1 := x, y
				if prev == 'Q' || prev == 'T' {
					x1, y1 = 2*x-lcx, 2*y-lcy
				}
				quad(x1, y1, a[0]+ox, a[1]+oy)
			case 'A':
				arcTo(&x, &y, a[0], a[1], a[2], a[3] != 0, a[4] != 0, a[5]+ox, a[6]+oy, lineTo)
			}
			prev = up
		}
	}
	flush()
	return subs
}

// arcNums parses arc arguments, where the two flags may be written without separators ("a1 1 0 011 1").
func arcNums(s string) []float64 {
	var out []float64
	pos := 0
	for {
		k := len(out) % 7
		for pos < len(s) && (s[pos] == ' ' || s[pos] == ',' || s[pos] == '\t' || s[pos] == '\n' || s[pos] == '\r') {
			pos++
		}
		if pos >= len(s) {
			return out
		}
		if k == 3 || k == 4 {
			if s[pos] == '0' || s[pos] == '1' {
				out = append(out, float64(s[pos]-'0'))
				pos++
				continue
			}
			return out
		}
		nums := svgNums(s[pos:])
		if len(nums) == 0 {
			return out
		}
		// find how much text the first number used
		j := pos
		if s[j] == '+' || s[j] == '-' {
			j++
		}
		dot, exp := false, false
		for j < len(s) {
			c := s[j]
			if c >= '0' && c <= '9' {
			} else if c == '.' && !dot && !exp {
				dot = true
			} else if (c == 'e' || c == 'E') && !exp {
				exp = true
				if j+1 < len(s) && (s[j+1] == '-' || s[j+1] == '+') {
					j++
				}
			} else {
				break
			}
			j++
		}
		out = append(out, nums[0])
		pos = j
	}
}

// arcTo flattens an elliptical arc (SVG endpoint parameterisation).
func arcTo(x, y *float64, rx, ry, phi float64, large, sweep bool, x2, y2 float64, lineTo func(float64, float64)) {
	x1, y1 := *x, *y
	if rx == 0 || ry == 0 || (x1 == x2 && y1 == y2) {
		lineTo(x2, y2)
		return
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	p := phi * math.Pi / 180
	cp, sp := math.Cos(p), math.Sin(p)
	dx, dy := (x1-x2)/2, (y1-y2)/2
	x1p := cp*dx + sp*dy
	y1p := -sp*dx + cp*dy
	l := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry)
	if l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	co := 0.0
	if den != 0 && num > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cxp, cyp := co*rx*y1p/ry, -co*ry*x1p/rx
	cx := cp*cxp - sp*cyp + (x1+x2)/2
	cy := sp*cxp + cp*cyp + (y1+y2)/2
	ang := func(ux, uy, vx, vy float64) float64 {
		a := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
		return a
	}
	t1 := ang(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dt := ang((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dt > 0 {
		dt -= 2 * math.Pi
	} else if sweep && dt < 0 {
		dt += 2 * math.Pi
	}
	n := max(4, int(math.Abs(dt)/(math.Pi/16)))
	for i := 1; i <= n; i++ {
		t := t1 + dt*float64(i)/float64(n)
		ex, ey := rx*math.Cos(t), ry*math.Sin(t)
		lineTo(cp*ex-sp*ey+cx, sp*ex+cp*ey+cy)
	}
	*x, *y = x2, y2
}

// strokePolys outlines polylines as rectangles per segment plus round joins,
// all wound the same way so a nonzero fill unions them.
func strokePolys(subs [][]pt, w float64, closed []bool) [][]pt {
	h := w / 2
	var out [][]pt
	join := func(c pt) {
		n := 10
		s := make([]pt, 0, n+1)
		for i := 0; i <= n; i++ {
			a := -float64(i) * 2 * math.Pi / float64(n)
			s = append(s, pt{c.x + h*math.Cos(a), c.y + h*math.Sin(a)})
		}
		out = append(out, s)
	}
	for si, s := range subs {
		for i := 1; i < len(s); i++ {
			a, b := s[i-1], s[i]
			dx, dy := b.x-a.x, b.y-a.y
			l := math.Hypot(dx, dy)
			if l == 0 {
				continue
			}
			nx, ny := -dy/l*h, dx/l*h
			out = append(out, []pt{{a.x + nx, a.y + ny}, {b.x + nx, b.y + ny}, {b.x - nx, b.y - ny}, {a.x - nx, a.y - ny}, {a.x + nx, a.y + ny}})
			if i < len(s)-1 || (si < len(closed) && closed[si]) {
				if w > 1.5 {
					join(b)
				}
			}
		}
	}
	// make every piece wind like the first rectangle
	if len(out) > 0 {
		sign := polyArea(out[0]) > 0
		for _, p := range out {
			if (polyArea(p) > 0) != sign {
				for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
					p[i], p[j] = p[j], p[i]
				}
			}
		}
	}
	return out
}

func polyArea(p []pt) float64 {
	a := 0.0
	for i := 1; i < len(p); i++ {
		a += p[i-1].x*p[i].y - p[i].x*p[i-1].y
	}
	return a
}

// ---- scanline rasterizer ----

type edge struct {
	x0, y0, x1, y1 float64
	dir            int
}

const svgSS = 4 // vertical subsamples per pixel

// covBox is anti-aliased coverage (0..1) for the pixels of a bounding box.
type covBox struct {
	x0, y0, w, h int
	v            []float32
}

func (c covBox) at(x, y int) float32 {
	x -= c.x0
	y -= c.y0
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return 0
	}
	return c.v[y*c.w+x]
}

// coverage rasterizes polygons within a w×h canvas, touching only their bounding box.
func coverage(polys [][]pt, w, h int, evenOdd bool) covBox {
	var edges []edge
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range polys {
		n := len(p)
		for i := 0; i < n; i++ {
			a, b := p[i], p[(i+1)%n]
			minX, maxX = math.Min(minX, a.x), math.Max(maxX, a.x)
			if a.y == b.y {
				continue
			}
			e := edge{a.x, a.y, b.x, b.y, 1}
			if a.y > b.y {
				e = edge{b.x, b.y, a.x, a.y, -1}
			}
			edges = append(edges, e)
			minY, maxY = math.Min(minY, e.y0), math.Max(maxY, e.y1)
		}
	}
	bx0 := max(0, int(math.Floor(minX)))
	bx1 := min(w, int(math.Ceil(maxX))+1)
	by0 := max(0, int(math.Floor(minY)))
	by1 := min(h, int(math.Ceil(maxY)))
	if len(edges) == 0 || bx1 <= bx0 || by1 <= by0 {
		return covBox{}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].y0 < edges[j].y0 })
	bw, bh := bx1-bx0, by1-by0
	out := covBox{x0: bx0, y0: by0, w: bw, h: bh, v: make([]float32, bw*bh)}
	type xing struct {
		x   float64
		dir int
	}
	var xs []xing
	first := 0 // edges before this index end above the current scanline
	for py := by0; py < by1; py++ {
		acc := out.v[(py-by0)*bw : (py-by0+1)*bw]
		for k := 0; k < svgSS; k++ {
			sy := float64(py) + (float64(k)+0.5)/svgSS
			for first < len(edges) && edges[first].y1 <= sy {
				first++ // ended; later scanlines are lower still
			}
			xs = xs[:0]
			for _, e := range edges[first:] {
				if e.y0 > sy {
					break
				}
				if sy < e.y1 {
					xs = append(xs, xing{e.x0 + (sy-e.y0)*(e.x1-e.x0)/(e.y1-e.y0), e.dir})
				}
			}
			if len(xs) < 2 {
				continue
			}
			for i := 1; i < len(xs); i++ { // insertion sort: lists are short
				for j := i; j > 0 && xs[j].x < xs[j-1].x; j-- {
					xs[j], xs[j-1] = xs[j-1], xs[j]
				}
			}
			wind := 0
			for i := 0; i < len(xs)-1; i++ {
				wind += xs[i].dir
				in := wind != 0
				if evenOdd {
					in = wind%2 != 0
				}
				if !in {
					continue
				}
				a := math.Max(float64(bx0), xs[i].x) - float64(bx0)
				b := math.Min(float64(bx1), xs[i+1].x) - float64(bx0)
				if b <= a {
					continue
				}
				ia, ib := int(a), int(b)
				if ia == ib {
					acc[ia] += float32((b - a) / svgSS)
					continue
				}
				acc[ia] += float32((float64(ia+1) - a) / svgSS)
				for x := ia + 1; x < ib; x++ {
					acc[x] += 1.0 / svgSS
				}
				if ib < bw {
					acc[ib] += float32((b - float64(ib)) / svgSS)
				}
			}
		}
		for x := range acc {
			if acc[x] > 1 {
				acc[x] = 1
			}
		}
	}
	return out
}

func fillPolys(dst *image.RGBA, polys [][]pt, evenOdd bool, col color.RGBA, alpha float64, clip []float32) {
	if alpha <= 0 {
		return
	}
	b := dst.Bounds()
	w, h := b.Dx(), b.Dy()
	cv := coverage(polys, w, h, evenOdd)
	for yy := 0; yy < cv.h; yy++ {
		y := cv.y0 + yy
		for xx := 0; xx < cv.w; xx++ {
			c := cv.v[yy*cv.w+xx]
			if c == 0 {
				continue
			}
			x := cv.x0 + xx
			a := float64(c) * alpha
			if clip != nil {
				a *= float64(clip[y*w+x])
			}
			if a <= 0 {
				continue
			}
			o := y*dst.Stride + x*4
			p := dst.Pix[o : o+4]
			p[0] = uint8(float64(p[0])*(1-a) + float64(col.R)*a + 0.5)
			p[1] = uint8(float64(p[1])*(1-a) + float64(col.G)*a + 0.5)
			p[2] = uint8(float64(p[2])*(1-a) + float64(col.B)*a + 0.5)
		}
	}
}

// looksLikeSVG reports whether data is an SVG document.
func looksLikeSVG(name string, b []byte) bool {
	if strings.HasSuffix(strings.ToLower(name), ".svg") {
		return true
	}
	head := b
	if len(head) > 512 {
		head = head[:512]
	}
	return bytes.Contains(bytes.ToLower(head), []byte("<svg"))
}
