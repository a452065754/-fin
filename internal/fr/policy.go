package fr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Policy groups the capabilities that a deployment may want to keep closed.
//
// Everything here is off by default: a template is untrusted input, and a
// template that can run SQL or read another report's cells can reach further
// than the person who opened the page expects. A deployment that trusts its
// templates turns the relevant switch on explicitly.
//
// The switch is read from a JSON file and from environment variables; the
// environment wins so an operator can override a shipped file.
type Policy struct {
	// FormulaSQL allows the SQL() formula function to run a query against a
	// configured datasource. It is the riskiest switch because the SQL text
	// comes from the template.
	FormulaSQL bool `json:"formulaSql"`
	// CrossReport allows VALUE("report name", row, column) to read a cell
	// from another report.
	CrossReport bool `json:"crossReport"`
	// CrossSheet allows the 'sheet name'!A1 reference syntax.
	CrossSheet bool `json:"crossSheet"`
	// MaxSQLRows caps how many rows a formula query may return. Zero means
	// the default cap.
	MaxSQLRows int `json:"maxSqlRows"`
	// MaxSQLTimeoutSeconds caps how long a formula query may run.
	MaxSQLTimeoutSeconds int `json:"maxSqlTimeoutSeconds"`
}

// DefaultMaxSQLRows is the row cap used when the policy does not set one.
const DefaultMaxSQLRows = 2000

// DefaultMaxSQLTimeoutSeconds is the time cap used when the policy does not
// set one.
const DefaultMaxSQLTimeoutSeconds = 30

// DefaultPolicy returns the policy used when nothing is configured: every
// restricted capability is closed.
func DefaultPolicy() Policy {
	return Policy{
		FormulaSQL:           false,
		CrossReport:          false,
		CrossSheet:           false,
		MaxSQLRows:           DefaultMaxSQLRows,
		MaxSQLTimeoutSeconds: DefaultMaxSQLTimeoutSeconds,
	}
}

// LoadPolicy reads the policy from a JSON file and then from the
// environment. The file is optional; a missing file is not an error.
//
// The file is looked up as:
//
//	<exe dir>/config.json
//	<exe dir>/../config.json
//	<workdir>/config.json
//
// The environment variables are:
//
//	RV_ALLOW_FORMULA_SQL   1/true/yes/on enables the SQL() function
//	RV_ALLOW_CROSS_REPORT  1/true/yes/on enables cross-report cell reads
//	RV_ALLOW_CROSS_SHEET   1/true/yes/on enables cross-sheet references
//	RV_MAX_SQL_ROWS        row cap for formula queries
//	RV_MAX_SQL_TIMEOUT     time cap for formula queries, in seconds
func LoadPolicy() (Policy, error) {
	p := DefaultPolicy()
	paths := policyConfigPaths()
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var file Policy
		if err := json.Unmarshal(data, &file); err != nil {
			return p, fmt.Errorf("配置文件 %s 解析失败: %w", path, err)
		}
		// Only the switches present in the file take effect; the rest keep
		// their defaults.
		if file.MaxSQLRows > 0 {
			p.MaxSQLRows = file.MaxSQLRows
		}
		if file.MaxSQLTimeoutSeconds > 0 {
			p.MaxSQLTimeoutSeconds = file.MaxSQLTimeoutSeconds
		}
		p.FormulaSQL = file.FormulaSQL
		p.CrossReport = file.CrossReport
		p.CrossSheet = file.CrossSheet
		break
	}
	// The environment overrides the file.
	if v, ok := os.LookupEnv("RV_ALLOW_FORMULA_SQL"); ok {
		p.FormulaSQL = parseBool(v, p.FormulaSQL)
	}
	if v, ok := os.LookupEnv("RV_ALLOW_CROSS_REPORT"); ok {
		p.CrossReport = parseBool(v, p.CrossReport)
	}
	if v, ok := os.LookupEnv("RV_ALLOW_CROSS_SHEET"); ok {
		p.CrossSheet = parseBool(v, p.CrossSheet)
	}
	if v, ok := os.LookupEnv("RV_MAX_SQL_ROWS"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			p.MaxSQLRows = n
		}
	}
	if v, ok := os.LookupEnv("RV_MAX_SQL_TIMEOUT"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			p.MaxSQLTimeoutSeconds = n
		}
	}
	return p, nil
}

// policyConfigPaths lists the config.json locations that are looked up.
func policyConfigPaths() []string {
	var out []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		out = append(out, filepath.Join(dir, "config.json"))
		out = append(out, filepath.Join(dir, "..", "config.json"))
	}
	if wd, err := os.Getwd(); err == nil {
		out = append(out, filepath.Join(wd, "config.json"))
	}
	return out
}

// parseBool reads a switch value written as 1/true/yes/on or 0/false/no/off.
func parseBool(v string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// SQLRowLimit returns the row cap for a formula query.
func (p Policy) SQLRowLimit() int {
	if p.MaxSQLRows > 0 {
		return p.MaxSQLRows
	}
	return DefaultMaxSQLRows
}

// Summary describes the policy for the health endpoint and the UI.
func (p Policy) Summary() map[string]any {
	return map[string]any{
		"formulaSql":          p.FormulaSQL,
		"crossReport":         p.CrossReport,
		"crossSheet":          p.CrossSheet,
		"maxSqlRows":          p.SQLRowLimit(),
		"maxSqlTimeoutSeconds": p.MaxSQLTimeoutSeconds,
	}
}

// PolicyError is returned when a template uses a capability the policy
// keeps closed. The message names the switch so the operator knows what to
// turn on.
type PolicyError struct {
	// Feature is the short name of the blocked capability.
	Feature string
	// Switch is the configuration key that enables it.
	Switch string
}

func (e *PolicyError) Error() string {
	return fmt.Sprintf("受限能力 %s 已被安全防护开关关闭（如需开启请设置 %s）", e.Feature, e.Switch)
}
