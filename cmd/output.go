package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type envelope struct {
	Version string `json:"version"`
	Results any    `json:"results"`
}

type kv struct {
	k string
	v string
}

func writeJSON(w io.Writer, data any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope{
		Version: "0.1",
		Results: data,
	})
}

func frontmatter(w io.Writer, meta []kv, content string) error {
	if _, err := fmt.Fprintln(w, "---"); err != nil {
		return err
	}
	for _, m := range meta {
		if _, err := fmt.Fprintf(w, "%s: %s\n", m.k, formatFrontmatterValue(m.v)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "---"); err != nil {
		return err
	}
	if content == "" {
		return nil
	}
	if _, err := fmt.Fprint(w, content); err != nil {
		return err
	}
	if !strings.HasSuffix(content, "\n") {
		_, err := fmt.Fprintln(w)
		return err
	}
	return nil
}

func formatFrontmatterValue(v string) string {
	if v == "" {
		return `""`
	}
	if strings.ContainsAny(v, "\r\n\t") ||
		strings.TrimSpace(v) != v ||
		strings.HasPrefix(v, "#") ||
		strings.HasPrefix(v, "-") ||
		strings.HasPrefix(v, "?") ||
		strings.HasPrefix(v, ":") ||
		strings.HasPrefix(v, "{") ||
		strings.HasPrefix(v, "[") ||
		strings.HasPrefix(v, "&") ||
		strings.HasPrefix(v, "*") ||
		strings.HasPrefix(v, "!") ||
		strings.HasPrefix(v, "|") ||
		strings.HasPrefix(v, ">") ||
		strings.HasPrefix(v, "@") ||
		strings.HasPrefix(v, "`") ||
		strings.Contains(v, ": ") ||
		strings.Contains(v, " #") ||
		v == "---" ||
		v == "..." {
		return strconv.Quote(v)
	}
	return v
}

func truncateText(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	if maxChars <= 3 {
		return safeBytePrefix(s, maxChars)
	}
	return strings.TrimSpace(safeBytePrefix(s, maxChars-3)) + "..."
}

func safeBytePrefix(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	end := 0
	for i := range s {
		if i > maxBytes {
			break
		}
		end = i
	}
	return s[:end]
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
