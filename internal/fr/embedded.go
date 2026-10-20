package fr

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// DecodeFineString decodes FineReport's embedded data encoding:
// custom Ascii85 -> raw DEFLATE -> UTF-8 text.
func DecodeFineString(s string) ([]byte, error) {
	raw := decodeAscii85Java(s)
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty ascii85 payload")
	}
	zr := flate.NewReader(bytes.NewReader(raw))
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, 64<<20))
	if err != nil {
		// Some bundled demo payloads are truncated. FineReport's own
		// Inflater returns the decoded prefix, so keep it and let the caller
		// decide whether the partial rows are usable.
		if len(out) > 0 {
			return out, fmt.Errorf("inflate: %w", err)
		}
		return nil, fmt.Errorf("inflate: %w", err)
	}
	return out, nil
}

// decodeAscii85Java mirrors com.fr.stable.Ascii85InputStream. Its handling of
// partial groups and of '~'/'x' terminators differs from the standard codec.
func decodeAscii85Java(s string) []byte {
	var out []byte
	b := []byte(s)
	pos := 0
	skipSpace := func() {
		for pos < len(b) && (b[pos] == '\n' || b[pos] == '\r' || b[pos] == ' ') {
			pos++
		}
	}
	ascii := [5]byte{}
	for pos < len(b) {
		skipSpace()
		if pos >= len(b) {
			break
		}
		c := b[pos]
		pos++
		if c == '~' || c == 'x' {
			break
		}
		if c == 'z' {
			out = append(out, 0, 0, 0, 0)
			continue
		}
		ascii[0] = c
		i := 1
		terminated := false
		for i < 5 {
			skipSpace()
			if pos >= len(b) {
				return out
			}
			c = b[pos]
			pos++
			ascii[i] = c
			if c == '~' || c == 'x' {
				terminated = true
				break
			}
			i++
		}
		n := 4
		if terminated {
			// FineReport's Ascii85InputStream drops the final partial byte and
			// pads the group with '!' before decoding.
			n = i - 1
			if n <= 0 {
				break
			}
			for k := i + 1; k < 5; k++ {
				ascii[k] = '!'
			}
		}
		var v uint64
		for k := 0; k < 5; k++ {
			d := int(ascii[k]) - 33
			if d < 0 || d > 93 {
				return out
			}
			v = v*85 + uint64(d)
		}
		u := uint32(v)
		full := []byte{byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)}
		out = append(out, full[:n]...)
		if terminated {
			break
		}
	}
	return out
}

// EmbeddedColumns splits the ColumnNames payload. FineReport separates column
// names with ",,.,," but older files simply use ",".
func EmbeddedColumns(names string) []string {
	names = strings.TrimSpace(names)
	if names == "" {
		return nil
	}
	var parts []string
	if strings.Contains(names, ",,.,,") {
		parts = strings.Split(names, ",,.,,")
	} else if strings.Contains(names, "\u0001") {
		parts = strings.Split(names, "\u0001")
	} else if strings.Contains(names, ",") {
		parts = strings.Split(names, ",")
	} else {
		parts = []string{names}
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.TrimSuffix(p, ","))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseEmbeddedRows decodes a tab/newline separated RowData payload.
func ParseEmbeddedRows(text string, types []string) [][]any {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	rows := make([][]any, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		row := make([]any, len(cols))
		for i, v := range cols {
			row[i] = convertEmbeddedValue(v, typeAt(types, i))
		}
		rows = append(rows, row)
	}
	return rows
}

func typeAt(types []string, i int) string {
	if i < len(types) {
		return strings.TrimSpace(types[i])
	}
	return ""
}

func convertEmbeddedValue(v, typ string) any {
	typ = strings.ToLower(typ)
	switch {
	case strings.Contains(typ, "integer"), strings.Contains(typ, "long"), strings.Contains(typ, "short"):
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n
		}
	case strings.Contains(typ, "double"), strings.Contains(typ, "float"), strings.Contains(typ, "decimal"), strings.Contains(typ, "number"):
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	case strings.Contains(typ, "boolean"):
		return strings.EqualFold(strings.TrimSpace(v), "true")
	}
	return v
}
