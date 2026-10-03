package main

import (
	"fmt"
	"image/color"
)

// The settings screen: ↑↓ choose a setting, ←→ change it (L1/R1 in bigger
// steps), B saves and goes back.

type setting struct {
	label  string
	help   string
	value  func(c *Config) string
	change func(c *Config, dir int, big bool)
}

var maxIntervals = []int{7, 14, 30, 60, 90, 180, 365, 730, 1825, 3650, 36500}

var textSizes = []struct {
	size  int
	label string
}{{18, "Small"}, {22, "Medium"}, {26, "Large"}, {30, "Huge"}}

func stepInt(v, dir int, small, big int, isBig bool, lo, hi int) int {
	d := small
	if isBig {
		d = big
	}
	v = (v/d)*d + dir*d // snap to the step, then move
	if dir < 0 && v < 0 {
		v = 0
	}
	return min(hi, max(lo, v))
}

func intervalLabel(d int) string {
	switch {
	case d >= 36500:
		return "No limit"
	case d >= 365 && d%365 == 0:
		if d == 365 {
			return "1 year"
		}
		return fmt.Sprintf("%d years", d/365)
	case d >= 30 && d%30 == 0:
		if d == 30 {
			return "1 month"
		}
		return fmt.Sprintf("%d months", d/30)
	case d%7 == 0:
		if d == 7 {
			return "1 week"
		}
		return fmt.Sprintf("%d weeks", d/7)
	}
	return fmt.Sprintf("%d days", d)
}

var settings = []setting{
	{
		label: "New cards per day",
		help:  "Cards you haven't seen yet, introduced each day in each deck file. Every new card adds reviews on later days.",
		value: func(c *Config) string { return fmt.Sprint(c.NewPerDay) },
		change: func(c *Config, dir int, big bool) {
			c.NewPerDay = stepInt(c.NewPerDay, dir, 5, 50, big, 0, 999)
		},
	},
	{
		label: "Reviews per day",
		help:  "The most reviews shown in a day. About 10 times your new cards keeps up; extra reviews wait for the next day.",
		value: func(c *Config) string { return commas(c.ReviewsPerDay) },
		change: func(c *Config, dir int, big bool) {
			c.ReviewsPerDay = stepInt(c.ReviewsPerDay, dir, 50, 500, big, 10, 9999)
		},
	},
	{
		label: "Target recall",
		help:  "How likely you are to still remember a card when it comes back (FSRS). Higher means shorter intervals and more reviews; 90% is the usual choice.",
		value: func(c *Config) string { return fmt.Sprintf("%.0f%%", c.DesiredRetention*100) },
		change: func(c *Config, dir int, big bool) {
			p := int(c.DesiredRetention*100 + 0.5)
			if big {
				dir *= 5
			}
			c.DesiredRetention = float64(min(97, max(70, p+dir))) / 100
		},
	},
	{
		label: "Longest interval",
		help:  "Cards never wait longer than this between reviews, even when FSRS would wait longer.",
		value: func(c *Config) string { return intervalLabel(c.MaxInterval) },
		change: func(c *Config, dir int, big bool) {
			i := len(maxIntervals) - 1
			for j, v := range maxIntervals {
				if c.MaxInterval <= v {
					i = j
					break
				}
			}
			c.MaxInterval = maxIntervals[min(len(maxIntervals)-1, max(0, i+dir))]
		},
	},
	{
		label: "New card order",
		help:  "Random mixes new cards from the whole deck file. Deck order shows them in the order the deck lists them.",
		value: func(c *Config) string {
			if c.NewOrder == "random" {
				return "Random"
			}
			return "Deck order"
		},
		change: func(c *Config, dir int, big bool) {
			if c.NewOrder == "random" {
				c.NewOrder = "deck"
			} else {
				c.NewOrder = "random"
			}
		},
	},
	{
		label: "Text size",
		help:  "Size of the text on cards. SELECT on a card changes it too.",
		value: func(c *Config) string {
			for _, t := range textSizes {
				if c.TextSize <= t.size {
					return t.label
				}
			}
			return textSizes[len(textSizes)-1].label
		},
		change: func(c *Config, dir int, big bool) {
			i := len(textSizes) - 1
			for j, t := range textSizes {
				if c.TextSize <= t.size {
					i = j
					break
				}
			}
			c.TextSize = textSizes[min(len(textSizes)-1, max(0, i+dir))].size
		},
	},
	{
		label: "Sounds",
		help:  "Play a card's sounds as soon as it's shown, or only when you press START.",
		value: func(c *Config) string {
			if c.AutoplayAudio {
				return "Play automatically"
			}
			return "Only with START"
		},
		change: func(c *Config, dir int, big bool) { c.AutoplayAudio = !c.AutoplayAudio },
	},
}

func (u *UI) openSettings() {
	u.setFrom = u.scr
	u.setSel = 0
	u.scr = scrSettings
}

func (u *UI) keySettings(k int) {
	switch k {
	case BtnUp:
		u.setSel = (u.setSel - 1 + len(settings)) % len(settings)
	case BtnDown:
		u.setSel = (u.setSel + 1) % len(settings)
	case BtnLeft, BtnRight, BtnL1, BtnR1, BtnA:
		dir := 1
		if k == BtnLeft || k == BtnL1 {
			dir = -1
		}
		settings[u.setSel].change(&u.env.Config, dir, k == BtnL1 || k == BtnR1)
	case BtnB, BtnStart:
		u.closeSettings()
	}
}

// closeSettings saves the settings and puts them to work.
func (u *UI) closeSettings() {
	u.env.SaveConfig()
	applyConfig(u.env.Config)
	u.refreshCounts()
	u.scr = u.setFrom
	if u.scr == scrDecks && u.cur != nil {
		u.buildDecks()
	}
	u.showToast("SETTINGS SAVED")
}

func (u *UI) drawSettings(c *Canvas) {
	x, y, w, h := u.sheet(c)
	hf := cardFont(u.px(12), 1)
	c.TextSet(hf, x+u.px(46), y+u.px(12), "SETTINGS", inkSoft)
	s := "FSRS-6 SCHEDULING"
	c.TextSet(hf, x+w-u.px(18)-hf.Width(s), y+u.px(12), s, inkSoft)
	top := y + u.px(34)
	c.Fill(x, top-u.px(2), w, u.px(2), ruleBlue)
	rh := u.deckRowH()
	lf := cardFont(u.px(17), 0)
	vf := cardFont(u.px(17), 1)
	right := x + w - u.px(18)
	for i, st := range settings {
		ry := top + i*rh
		sel := i == u.setSel
		if sel {
			c.Fill(x+u.px(36), ry+u.px(4), w-u.px(44), rh-u.px(6), colMark)
		}
		c.Fill(x, ry+rh-1, w, 1, ruleBlue)
		c.TextSet(lf, x+u.px(46), ry+(rh-lf.height)/2, st.label, inkCol)
		v := st.value(&u.env.Config)
		vx := right - vf.Width(v)
		if sel {
			// arrows show the value can be changed with left/right
			ah := u.px(12)
			cy := ry + rh/2
			arrow(c, right-ah/2, cy, ah, 1, ruleRed)
			vx = right - ah - u.px(10) - vf.Width(v)
			arrow(c, vx-u.px(10)-ah/2, cy, ah, -1, ruleRed)
		}
		c.TextSet(vf, vx, ry+(rh-vf.height)/2, v, inkCol)
	}
	// what the chosen setting does
	bf := cardFont(u.px(14), 0)
	ly := top + len(settings)*rh + u.px(14)
	for _, l := range bf.Wrap(settings[u.setSel].help, w-u.px(70)) {
		if ly+bf.height > y+h-u.px(8) {
			break
		}
		c.TextSet(bf, x+u.px(46), ly, l, inkSoft)
		ly += bf.height + u.px(4)
	}
}

// arrow draws a small solid triangle centred on (cx, cy), h pixels tall,
// pointing right (dir 1) or left (dir -1).
func arrow(c *Canvas, cx, cy, h, dir int, col color.RGBA) {
	half := h / 2
	for i := 0; i <= half; i++ {
		// column i from the flat side has a height shrinking to the point
		ch := h - 2*i
		x := cx - half/2 + i
		if dir < 0 {
			x = cx + half/2 - i
		}
		c.Fill(x, cy-ch/2, 1, max(1, ch), col)
	}
}
