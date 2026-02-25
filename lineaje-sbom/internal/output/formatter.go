// Package output formats the Lineaje explain API response as table or JSON.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/olekukonko/tablewriter"
)

// Write formats responseBytes as table (default) or JSON and writes to w.
// For table format, parses the API response to extract meta_data (per-component)
// and answer; for JSON, writes the raw response (indented).
func Write(w io.Writer, responseBytes []byte, format string) error {
	if format == "json" {
		var raw map[string]interface{}
		if err := json.Unmarshal(responseBytes, &raw); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(raw)
	}
	return writeTable(w, responseBytes)
}

// writeTable parses the explain API response and renders a table of components
// (PURL, status, vulnerability counts) plus answer if present.
func writeTable(w io.Writer, responseBytes []byte) error {
	var resp struct {
		Answer   interface{}            `json:"answer"`
		MetaData map[string]interface{} `json:"meta_data"`
	}
	if err := json.Unmarshal(responseBytes, &resp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	table := tablewriter.NewWriter(w)
	table.Header("PURL", "Status", "Reason", "Critical", "High", "Medium", "Low", "Total")

	if resp.MetaData != nil {
		for key, val := range resp.MetaData {
			row := rowFromComponent(key, val)
			if row != nil {
				_ = table.Append(row)
			}
		}
	}

	if err := table.Render(); err != nil {
		return err
	}

	if resp.Answer != nil {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Answer:")
		fmt.Fprintln(w, formatAnswer(resp.Answer))
	}
	return nil
}

func rowFromComponent(key string, val interface{}) []string {
	m, ok := val.(map[string]interface{})
	if !ok {
		return nil
	}
	purl := key
	if p, ok := m["purl"].(string); ok && p != "" {
		purl = p
	}
	status := ""
	reason := ""
	if s, ok := m["status"].(map[string]interface{}); ok {
		if c, ok := s["code"].(string); ok {
			status = c
		}
		if r, ok := s["reason"].(string); ok {
			reason = r
		}
	}
	crit, high, med, low, total := "0", "0", "0", "0", "0"
	if vc, ok := m["vulnerability_count"].(map[string]interface{}); ok {
		if n, ok := vc["total"].(float64); ok {
			total = strconv.Itoa(int(n))
		}
		if bySev, ok := vc["by_severity"].(map[string]interface{}); ok {
			crit = getSeverity(bySev, "critical")
			high = getSeverity(bySev, "high")
			med = getSeverity(bySev, "medium")
			low = getSeverity(bySev, "low")
		}
	}
	return []string{purl, status, reason, crit, high, med, low, total}
}

func getSeverity(bySev map[string]interface{}, key string) string {
	if n, ok := bySev[key].(float64); ok {
		return strconv.Itoa(int(n))
	}
	return "0"
}

func formatAnswer(a interface{}) string {
	switch v := a.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	}
}
