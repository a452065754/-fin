package fr

import (
	"strings"
	"testing"
)

func TestParseSliceSpec(t *testing.T) {
	cases := []struct {
		spec       string
		row, col   string
		hasRow     bool
		hasCol     bool
		shouldFail bool
	}{
		{spec: "!0;!0", row: "!0", col: "!0", hasRow: true, hasCol: true},
		{spec: "!0", row: "!0", hasRow: true},
		{spec: "A4:-1,B4:+0", row: "A4:-1", col: "B4:+0", hasRow: true, hasCol: true},
		{spec: "A4:-1,B4:!-1", row: "A4:-1", col: "B4:!-1", hasRow: true, hasCol: true},
		{spec: ";B3:-1", col: "B3:-1", hasCol: true},
		{spec: ";!0", col: "!0", hasCol: true},
		{spec: "B3:-1", row: "B3:-1", hasRow: true},
		// A plain cell list is not a region.
		{spec: "E163,E164", row: "E163,E164", hasRow: true},
		{spec: "", shouldFail: true},
	}
	for _, c := range cases {
		sp, err := parseSliceSpec(c.spec)
		if c.shouldFail {
			if err == nil {
				t.Errorf("parseSliceSpec(%q) should fail", c.spec)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSliceSpec(%q) failed: %v", c.spec, err)
			continue
		}
		if sp.rowPart != c.row || sp.colPart != c.col {
			t.Errorf("parseSliceSpec(%q) = row %q col %q, want row %q col %q",
				c.spec, sp.rowPart, sp.colPart, c.row, c.col)
		}
		if sp.hasRow != c.hasRow || sp.hasCol != c.hasCol {
			t.Errorf("parseSliceSpec(%q) hasRow=%v hasCol=%v, want %v/%v",
				c.spec, sp.hasRow, sp.hasCol, c.hasRow, c.hasCol)
		}
	}
}

func TestResolveDirection(t *testing.T) {
	// A whole-area direction.
	d, err := resolveDirection("!0", axisRow, 4)
	if err != nil {
		t.Fatalf("resolve !0: %v", err)
	}
	if !d.whole {
		t.Error("!0 should cover the whole area")
	}
	// A relative offset counts along the expansion axis, so with the
	// current row as the base, "B4:-1" covers the level one above.
	d, err = resolveDirection("B4:-1", axisRow, 4)
	if err != nil {
		t.Fatalf("resolve B4:-1: %v", err)
	}
	if d.whole {
		t.Error("B4:-1 should not be whole")
	}
	if d.from != 3 || d.to != 4 {
		t.Errorf("B4:-1 = %d..%d, want 3..4", d.from, d.to)
	}
	// The column axis shares the expansion axis with the row direction,
	// so an offset steps through the expanded rows as well.
	d, err = resolveDirection("B4:+0", axisCol, 7)
	if err != nil {
		t.Fatalf("resolve B4:+0: %v", err)
	}
	if d.from != 7 || d.to != 7 {
		t.Errorf("B4:+0 = %d..%d, want 7..7", d.from, d.to)
	}
	// A broken direction is an error.
	if _, err := resolveDirection("B4", axisRow, 4); err == nil {
		t.Error("expected a direction without an offset to fail")
	}
	if _, err := resolveDirection("ZZZZ:-1", axisRow, 4); err == nil {
		t.Error("expected an invalid base cell to fail")
	}
}

func TestPolicyDefaultsAreClosed(t *testing.T) {
	p := DefaultPolicy()
	if p.FormulaSQL || p.CrossReport || p.CrossSheet {
		t.Errorf("every restricted capability must be closed by default: %+v", p)
	}
	if p.SQLRowLimit() != DefaultMaxSQLRows {
		t.Errorf("row limit = %d, want %d", p.SQLRowLimit(), DefaultMaxSQLRows)
	}
}

func TestPolicyErrorNamesTheSwitch(t *testing.T) {
	err := &PolicyError{Feature: "SQL() 公式函数", Switch: "RV_ALLOW_FORMULA_SQL=1"}
	msg := err.Error()
	if !strings.Contains(msg, "SQL() 公式函数") {
		t.Errorf("message should name the feature: %s", msg)
	}
	if !strings.Contains(msg, "RV_ALLOW_FORMULA_SQL=1") {
		t.Errorf("message should name the switch: %s", msg)
	}
}

func TestPolicySummary(t *testing.T) {
	p := DefaultPolicy()
	p.FormulaSQL = true
	sum := p.Summary()
	if sum["formulaSql"] != true {
		t.Errorf("summary = %v", sum)
	}
	if sum["maxSqlRows"] != DefaultMaxSQLRows {
		t.Errorf("summary row cap = %v", sum["maxSqlRows"])
	}
}

func TestParseSliceOffset(t *testing.T) {
	cases := map[string]int{
		"-1": -1,
		"1":  1,
		"0":  0,
		"!-1": -1,
		"!0":  0,
	}
	for text, want := range cases {
		got, err := parseSliceOffset(text)
		if err != nil {
			t.Errorf("parseSliceOffset(%q) failed: %v", text, err)
			continue
		}
		if got != want {
			t.Errorf("parseSliceOffset(%q) = %d, want %d", text, got, want)
		}
	}
	if _, err := parseSliceOffset("x"); err == nil {
		t.Error("expected a bad offset to fail")
	}
}

func TestHasCellRefAfter(t *testing.T) {
	// "&A3" is an absolute reference, "&" followed by a name is a
	// concatenation, and "A" alone is not a cell reference.
	if !hasCellRefAfter("A3 rest", 0) {
		t.Error("A3 should be a cell reference")
	}
	if !hasCellRefAfter("$A$3 rest", 0) {
		t.Error("$A$3 should be a cell reference")
	}
	if hasCellRefAfter("name rest", 0) {
		t.Error("name is not a cell reference")
	}
	if hasCellRefAfter("1 rest", 0) {
		t.Error("a number is not a cell reference")
	}
}
