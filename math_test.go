package main

import "testing"

func TestMathLite(t *testing.T) {
	cases := map[string]string{
		`The \(\text{coulomb}\) [\(\text{C}\)]`:                       "The coulomb [C]",
		`\(\frac 13 e\)`:                                              "1/3 e",
		`\(F = \frac{q_1 q_2}{4\pi\epsilon_0 r^2}\)`:                  "F = (q₁ q₂)/(4πε₀ r²)",
		`\(\vec{E}\cdot d\vec{A}\)`:                                   "E⃗· dA⃗",
		`\[\oint \vec E \cdot d\vec A = \frac{Q_{enc}}{\epsilon_0}\]`: "∮ E⃗ · dA⃗ = Q_(enc)/ε₀",
		`\(\sqrt{x^2+y^2}\)`:                                          "√(x²+y²)",
		`\(10^{-19}\,\text{C}\)`:                                      "10⁻¹⁹ C",
		`\(\left(\frac{a}{b}\right)\)`:                                "(a/b)",
		`no math here`:                                                "no math here",
		`\(\Omega\ \mu\text{F} \leq 5\)`:                              "Ω μF ≤ 5",
	}
	for in, want := range cases {
		inMath := false
		if got := mathify(in, &inMath); got != want {
			t.Errorf("mathify(%q) = %q, want %q", in, got, want)
		}
	}
	// math spanning tags: "\(" and "\)" arrive in separate text runs
	inMath := false
	a := mathify(`charge \(`, &inMath)
	b := mathify(`Q_1`, &inMath)
	c := mathify(`\) here`, &inMath)
	if a+b+c != "charge Q₁ here" {
		t.Errorf("spanning: %q", a+b+c)
	}
}
