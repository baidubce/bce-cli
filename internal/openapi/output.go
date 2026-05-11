package openapi

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	jmespath "github.com/jmespath/go-jmespath"
)

// OutputFormat controls how the API response is rendered.
type OutputFormat string

const (
	FormatJSON  OutputFormat = "json"
	FormatTable OutputFormat = "table"
	FormatText  OutputFormat = "text"
)

// OutputOptions configures response rendering.
type OutputOptions struct {
	Format  OutputFormat
	Query   string   // JMESPath expression applied before formatting
	Cols    []string // column names for table output
	NoColor bool
}

// Print applies the JMESPath query (if any) then renders the result.
func Print(data map[string]interface{}, opts OutputOptions) error {
	var result interface{} = data

	if opts.Query != "" {
		filtered, err := jmespath.Search(opts.Query, result)
		if err != nil {
			return fmt.Errorf("--query: %w", err)
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

	// Encode to bytes first, then apply simple ANSI colouring
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(colorJSON(string(raw)))
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
		fmt.Println(fmt.Sprintf("%v", val))
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
					continue
				}
				if s[end] == '"' {
					break
				}
				end++
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
		for _, kw := range []string{"true", "false", "null"} {
			if strings.HasPrefix(s[i:], kw) {
				b.WriteString(purple + kw + reset)
				i += len(kw)
				goto next
			}
		}

		b.WriteByte(ch)
		i++
	next:
	}
	return b.String()
}
