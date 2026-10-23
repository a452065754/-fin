package fr

import "testing"

func TestFormatNumberMultiSection(t *testing.T) {
	cases := []struct {
		f        float64
		pattern  string
		want     string
	}{
		{230000, "$#,##0;($#,##0)", "$230,000"},
		{-230000, "$#,##0;($#,##0)", "($230,000)"},
		{320000, "$#,##0;($#,##0)", "$320,000"},
		{-90000, "#,##0.00", "-90,000.00"},
		{0.082, "0.0%", "8.2%"},
	}
	for _, c := range cases {
		if got := formatNumber(c.f, c.pattern); got != c.want {
			t.Errorf("formatNumber(%v, %q) = %q, want %q", c.f, c.pattern, got, c.want)
		}
	}
}
