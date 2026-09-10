package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// jsonField is one key/value pair of an --format json response. A slice
// (rather than a map) is used to build the response so the exact key order the
// plan specifies is preserved — Go's map iteration order is randomised, so
// marshalling a map would not guarantee it.
type jsonField struct {
	key   string
	value any
}

// writeAuthJSON renders an ordered set of fields as a single indented JSON
// object and writes it to the command's stdout.
func writeAuthJSON(cmd *cobra.Command, fields []jsonField) error {
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, f := range fields {
		key, err := json.Marshal(f.key)
		if err != nil {
			return err
		}
		val, err := json.Marshal(f.value)
		if err != nil {
			return err
		}
		buf.WriteString("  ")
		buf.Write(key)
		buf.WriteString(": ")
		buf.Write(val)
		if i < len(fields)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}")
	fmt.Fprintln(cmd.OutOrStdout(), buf.String())
	return nil
}

// authFormat reads and validates the --format flag shared by the auth
// subcommands. An invalid value is a usage error, not a silent fallback.
func authFormat(cmd *cobra.Command) (string, error) {
	format, _ := cmd.Flags().GetString("format")
	switch format {
	case "text", "json":
		return format, nil
	default:
		return "", fmt.Errorf("invalid --format %q: must be text or json", format)
	}
}
