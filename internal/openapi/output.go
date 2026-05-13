package openapi

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	jmespath "github.com/jmespath/go-jmespath"
)

// ParseOutput parses the --output flag value into format, column list, and row-extraction path.
//
// Supported forms:
//
//	"json"                             → FormatJSON
//	"text"                             → FormatText
//	"table"                            → FormatTable, columns auto-detected
//	"cols=id,name"                     → FormatTable, specified columns
//	"rows=vpcs"                        → FormatTable, JMESPath row extraction
//	"table cols=id,name rows=vpcs"     → FormatTable, columns + row extraction
func parseOutput(raw string) (format OutputFormat, cols []string, rows string, err error) {
	format = FormatJSON
	for _, part := range strings.Fields(raw) {
		switch {
		case part == "json":
			format = FormatJSON
		case part == "text":
			format = FormatText
		case part == "table":
			format = FormatTable
		case strings.HasPrefix(part, "cols="):
			format = FormatTable
			cols = strings.Split(strings.TrimPrefix(part, "cols="), ",")
		case strings.HasPrefix(part, "rows="):
			format = FormatTable
			rows = strings.TrimPrefix(part, "rows=")
		default:
			err = fmt.Errorf("invalid --output value %q: supported formats: json, text, table, table cols=<c1,c2> rows=<jmespath>", part)
			return
		}
	}
	return
}
type OutputFormat string

const (
	FormatJSON  OutputFormat = "json"
	FormatTable OutputFormat = "table"
	FormatText  OutputFormat = "text"
)

// OutputOptions configures response rendering.
type OutputOptions struct {
	Format  OutputFormat
	Query   string   // --query: JMESPath applied first to the raw response
	Rows    string   // rows=: JMESPath applied after Query for table row extraction
	Cols    []string // column names for table output
	NoColor bool
}

// Print applies --query then rows= (if set) and renders the result.
func Print(data map[string]interface{}, opts OutputOptions) error {
	var result interface{} = data

	if opts.Query != "" {
		filtered, err := jmespath.Search(opts.Query, result)
		if err != nil {
			return fmt.Errorf("--query: %w", err)
		}
		result = filtered
	}

	if opts.Rows != "" {
		filtered, err := jmespath.Search(opts.Rows, result)
		if err != nil {
			return fmt.Errorf("rows=: %w", err)
		}
		result = filtered
	}

	switch opts.Format {
	case FormatTable:
		return printTable(result, opts.Cols)
	case FormatText:
		printText(result)
		return nil
	default:
		return printJSON(result, !opts.NoColor && isTerminal())
	}
}

func printJSON(v interface{}, color bool) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)

	if !color {
		return enc.Encode(v)
	}

	// Encode via json.Encoder (SetEscapeHTML=false) into a buffer so that the
	// colour path produces identical byte output to the non-colour path — the
	// only difference is ANSI codes wrapped around tokens.
	var buf strings.Builder
	enc2 := json.NewEncoder(&buf)
	enc2.SetIndent("", "  ")
	enc2.SetEscapeHTML(false)
	if err := enc2.Encode(v); err != nil {
		return err
	}
	// Encode appends a trailing newline; trim it so fmt.Println adds exactly one.
	fmt.Println(colorJSON(strings.TrimRight(buf.String(), "\n")))
	return nil
}

func printTable(v interface{}, cols []string) error {
	rows, ok := v.([]interface{})
	if !ok {
		// single object — wrap in a slice
		if m, ok := v.(map[string]interface{}); ok {
			rows = []interface{}{m}
		} else {
			return printJSON(v, false)
		}
	}

	// Auto-detect columns from first row when --cols is not specified.
	if len(cols) == 0 && len(rows) > 0 {
		if m, ok := rows[0].(map[string]interface{}); ok {
			cols = make([]string, 0, len(m))
			for k := range m {
				cols = append(cols, k)
			}
			sort.Strings(cols)
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(cols, "\t"))
	for _, row := range rows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		vals := make([]string, len(cols))
		for i, col := range cols {
			if val, ok := m[col]; ok {
				vals[i] = fmt.Sprintf("%v", val)
			}
		}
		fmt.Fprintln(w, strings.Join(vals, "\t"))
	}
	return w.Flush()
}

func printText(v interface{}) {
	switch val := v.(type) {
	case string:
		fmt.Println(val)
	case nil:
		// nothing
	default:
		fmt.Printf("%v\n", val)
	}
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// colorJSON applies minimal ANSI colours to a pre-indented JSON string.
// Keys → bold blue, string values → green, numbers → yellow, booleans/null → magenta.
func colorJSON(s string) string {
	const (
		reset  = "\033[0m"
		blue   = "\033[1;34m" // object keys
		green  = "\033[0;32m" // string values
		yellow = "\033[0;33m" // numbers
		purple = "\033[0;35m" // true / false / null
	)

	var b strings.Builder
	i := 0
	n := len(s)

	for i < n {
		ch := s[i]

		// String token
		if ch == '"' {
			end := i + 1
			for end < n {
				if s[end] == '\\' {
					end += 2
					if end >= n {
						break
					}
					continue
				}
				if s[end] == '"' {
					break
				}
				end++
			}
			// Guard against unterminated string at EOF.
			if end >= n {
				end = n - 1
			}
			token := s[i : end+1]
			// Peek past whitespace to see if a colon follows → it's a key
			j := end + 1
			for j < n && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			if j < n && s[j] == ':' {
				b.WriteString(blue + token + reset)
			} else {
				b.WriteString(green + token + reset)
			}
			i = end + 1
			continue
		}

		// Number token
		if (ch >= '0' && ch <= '9') || ch == '-' {
			end := i + 1
			for end < n && (s[end] >= '0' && s[end] <= '9' || s[end] == '.' || s[end] == 'e' || s[end] == 'E' || s[end] == '+' || s[end] == '-') {
				end++
			}
			b.WriteString(yellow + s[i:end] + reset)
			i = end
			continue
		}

		// Keyword tokens: true / false / null
		matched := false
		for _, kw := range []string{"true", "false", "null"} {
			if strings.HasPrefix(s[i:], kw) {
				b.WriteString(purple + kw + reset)
				i += len(kw)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(ch)
			i++
		}
	}
	return b.String()
}
