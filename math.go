package main

// "Math lite": MathJax isn't available on the device, so TeX between \( \)
// and \[ \] is turned into readable Unicode text — Greek letters, operators,
// x², vₓ, fractions as a/b, √, vector arrows — instead of raw TeX.

import (
	"strings"
	"unicode"
)

// mathify converts the TeX parts of a text run. inMath carries an open \(
// across text runs (MathJax often spans tags like \(<b>x</b>\)).
func mathify(s string, inMath *bool) string {
	if !*inMath && !strings.Contains(s, `\(`) && !strings.Contains(s, `\[`) {
		return s
	}
	var b strings.Builder
	for len(s) > 0 {
		if *inMath {
			end := -1
			for _, d := range []string{`\)`, `\]`} {
				if i := strings.Index(s, d); i >= 0 && (end < 0 || i < end) {
					end = i
				}
			}
			if end < 0 {
				b.WriteString(texToText(s))
				return b.String()
			}
			b.WriteString(texToText(s[:end]))
			s = s[end+2:]
			*inMath = false
			continue
		}
		start := -1
		for _, d := range []string{`\(`, `\[`} {
			if i := strings.Index(s, d); i >= 0 && (start < 0 || i < start) {
				start = i
			}
		}
		if start < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		s = s[start+2:]
		*inMath = true
	}
	return b.String()
}

var texSymbols = map[string]string{
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε", "varepsilon": "ε", "zeta": "ζ",
	"eta": "η", "theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ", "nu": "ν",
	"xi": "ξ", "pi": "π", "rho": "ρ", "sigma": "σ", "tau": "τ", "upsilon": "υ", "phi": "φ", "varphi": "φ",
	"chi": "χ", "psi": "ψ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ", "Pi": "Π", "Sigma": "Σ",
	"Upsilon": "Υ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
	"cdot": "·", "times": "×", "div": "÷", "pm": "±", "mp": "∓", "le": "≤", "leq": "≤", "ge": "≥", "geq": "≥",
	"ne": "≠", "neq": "≠", "approx": "≈", "sim": "∼", "simeq": "≃", "equiv": "≡", "propto": "∝", "infty": "∞",
	"to": "→", "rightarrow": "→", "leftarrow": "←", "Rightarrow": "⇒", "Leftarrow": "⇐", "leftrightarrow": "↔",
	"Leftrightarrow": "⇔", "implies": "⇒", "iff": "⇔", "partial": "∂", "nabla": "∇", "int": "∫", "iint": "∬",
	"iiint": "∭", "oint": "∮", "sum": "∑", "prod": "∏", "circ": "°", "degree": "°", "cdots": "⋯", "ldots": "…",
	"dots": "…", "perp": "⊥", "parallel": "∥", "angle": "∠", "hbar": "ħ", "ell": "ℓ", "in": "∈", "notin": "∉",
	"cup": "∪", "cap": "∩", "subset": "⊂", "subseteq": "⊆", "forall": "∀", "exists": "∃", "neg": "¬",
	"prime": "′", "ast": "∗", "star": "⋆", "langle": "⟨", "rangle": "⟩", "vert": "|", "mid": "|", "|": "‖",
	"lvert": "|", "rvert": "|", "Vert": "‖", "{": "{", "}": "}", "%": "%", "$": "$", "#": "#", "&": "&", "_": "_",
	"oplus": "⊕", "otimes": "⊗", "uparrow": "↑", "downarrow": "↓", "gg": "≫", "ll": "≪", "therefore": "∴",
	"sin": "sin", "cos": "cos", "tan": "tan", "ln": "ln", "log": "log", "exp": "exp", "lim": "lim", "max": "max",
	"min": "min", "det": "det", "sinh": "sinh", "cosh": "cosh", "tanh": "tanh", "sec": "sec", "csc": "csc", "cot": "cot",
	"arcsin": "arcsin", "arccos": "arccos", "arctan": "arctan",
}

var texSpace = map[string]string{",": " ", ";": " ", ":": " ", "!": "", "quad": "  ", "qquad": "   ", " ": " ", "\\": " "}

// commands whose braces just hold text
var texTextCmds = map[string]bool{
	"text": true, "textrm": true, "textbf": true, "textit": true, "mathrm": true, "mathbf": true, "mathit": true,
	"mathsf": true, "mathtt": true, "mathcal": true, "mathbb": true, "boldsymbol": true, "bm": true, "operatorname": true,
	"mbox": true, "hbox": true, "textsf": true, "emph": true, "underline": true, "overline": true, "displaystyle": false,
	"color": false,
}

// commands dropped entirely (sizing and delimiters' size hints)
var texDrop = map[string]bool{
	"left": true, "right": true, "big": true, "Big": true, "bigg": true, "Bigg": true, "bigl": true, "bigr": true,
	"Bigl": true, "Bigr": true, "biggl": true, "biggr": true, "displaystyle": true, "textstyle": true,
	"limits": true, "nolimits": true, "middle": true,
}

var accents = map[string]string{"vec": "⃗", "hat": "̂", "bar": "̄", "dot": "̇", "ddot": "̈", "tilde": "̃", "overrightarrow": "⃗"}

const supChars = "0123456789+-=()niax"
const supOut = "⁰¹²³⁴⁵⁶⁷⁸⁹⁺⁻⁼⁽⁾ⁿⁱᵃˣ"
const subChars = "0123456789+-=()aeoxijnmprst"
const subOut = "₀₁₂₃₄₅₆₇₈₉₊₋₌₍₎ₐₑₒₓᵢⱼₙₘₚᵣₛₜ"

func mapScript(s, from, to string) (string, bool) {
	tr := []rune(to)
	var b strings.Builder
	for _, r := range s {
		if r == ' ' {
			continue
		}
		i := strings.IndexRune(from, r)
		if i < 0 {
			return "", false
		}
		b.WriteRune(tr[i])
	}
	return b.String(), true
}

type texParser struct {
	s   []rune
	pos int
}

func texToText(s string) string {
	p := &texParser{s: []rune(s)}
	out := p.expr(false)
	return strings.Join(strings.Fields(out), " ")
}

func (p *texParser) peek() rune {
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}
	return 0
}

func (p *texParser) skipSpace() {
	for p.pos < len(p.s) && unicode.IsSpace(p.s[p.pos]) {
		p.pos++
	}
}

// expr reads until the end, or the matching '}' when inGroup.
func (p *texParser) expr(inGroup bool) string {
	var b strings.Builder
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		switch c {
		case '}':
			if inGroup {
				p.pos++
				return b.String()
			}
			p.pos++
		case '{':
			p.pos++
			b.WriteString(p.expr(true))
		case '^', '_':
			p.pos++
			arg := p.arg()
			from, to := supChars, supOut
			if c == '_' {
				from, to = subChars, subOut
			}
			if m, ok := mapScript(arg, from, to); ok && m != "" {
				b.WriteString(m)
			} else if len([]rune(arg)) == 1 {
				b.WriteRune(c)
				b.WriteString(arg)
			} else {
				b.WriteString(string(c) + "(" + arg + ")")
			}
		case '\\':
			b.WriteString(p.command())
		case '&', '~':
			p.pos++
			b.WriteRune(' ')
		default:
			p.pos++
			b.WriteRune(c)
		}
	}
	return b.String()
}

// arg reads one argument: a {group}, a \command, or a single character.
func (p *texParser) arg() string {
	p.skipSpace()
	switch c := p.peek(); c {
	case 0:
		return ""
	case '{':
		p.pos++
		return p.expr(true)
	case '\\':
		return p.command()
	default:
		p.pos++
		return string(c)
	}
}

func (p *texParser) command() string {
	p.pos++ // backslash
	if p.pos >= len(p.s) {
		return ""
	}
	start := p.pos
	if unicode.IsLetter(p.s[p.pos]) {
		for p.pos < len(p.s) && unicode.IsLetter(p.s[p.pos]) {
			p.pos++
		}
	} else {
		p.pos++
	}
	name := string(p.s[start:p.pos])
	if sp, ok := texSpace[name]; ok {
		return sp
	}
	if texDrop[name] {
		return ""
	}
	switch name {
	case "frac", "dfrac", "tfrac", "cfrac":
		num, den := p.arg(), p.arg()
		return wrapFrac(num) + "/" + wrapFrac(den)
	case "sqrt":
		idx := ""
		if p.peek() == '[' {
			end := p.pos
			for end < len(p.s) && p.s[end] != ']' {
				end++
			}
			idx = string(p.s[p.pos+1 : min(end, len(p.s))])
			p.pos = min(end+1, len(p.s))
		}
		a := p.arg()
		if m, ok := mapScript(idx, supChars, supOut); ok && idx != "" {
			idx = m
		}
		if len([]rune(a)) == 1 {
			return idx + "√" + a
		}
		return idx + "√(" + a + ")"
	case "color", "textcolor":
		p.arg() // colour name
		if name == "textcolor" {
			return p.arg()
		}
		return ""
	case "begin", "end":
		p.arg() // environment name
		return " "
	}
	if acc, ok := accents[name]; ok {
		a := p.arg()
		if len([]rune(a)) == 1 {
			return a + acc
		}
		return a
	}
	if texTextCmds[name] {
		return p.arg()
	}
	if s, ok := texSymbols[name]; ok {
		return s
	}
	return name
}

func wrapFrac(s string) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= 1 || !strings.ContainsAny(s, " +-−·×/") {
		return s
	}
	return "(" + s + ")"
}
