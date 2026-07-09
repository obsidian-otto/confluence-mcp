package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Load parses a .env file at path and returns its key/value pairs.
// Lines starting with '#' and blank lines are skipped. Values may be wrapped
// in matching single or double quotes (quotes are stripped). No variable
// expansion is performed. A missing file returns (nil, nil); only parse
// errors are fatal. Malformed KEY=VALUE lines produce an error that names
// the offending key and redacts the value as "<value redacted>".
func Load(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	out := make(map[string]string)
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := strings.TrimRight(sc.Text(), " \t")
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		eq := strings.IndexByte(raw, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("invalid .env line %d (%s=<value redacted>)", lineNo, redactKey(raw))
		}
		key := strings.TrimSpace(raw[:eq])
		val, perr := parseValue(raw[eq+1:])
		if perr != nil {
			return nil, fmt.Errorf("invalid .env line %d (%s=<value redacted>): %v", lineNo, key, perr)
		}
		out[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}

// parseValue strips surrounding matching single/double quotes and trims
// trailing whitespace. An unterminated quote returns an error so the caller
// can produce the redacted line-N diagnostic.
func parseValue(v string) (string, error) {
	v = strings.TrimRight(v, " \t")
	if len(v) >= 2 {
		first, last := v[0], v[len(v)-1]
		if (first == '"' || first == '\'') && first == last {
			return v[1 : len(v)-1], nil
		}
	}
	if len(v) >= 1 && (v[0] == '"' || v[0] == '\'') {
		return "", fmt.Errorf("unterminated quote")
	}
	return v, nil
}

// redactKey returns a safe placeholder for a malformed line that has no '='.
// We never want to echo the user's input verbatim in an error path.
func redactKey(raw string) string {
	if raw == "" {
		return "<empty>"
	}
	return "<malformed>"
}
