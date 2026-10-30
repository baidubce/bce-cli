package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// captureOut redirects a command's stdout to a buffer for assertions.
func captureOut(cmd *cobra.Command) *bytes.Buffer {
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	return &buf
}

func TestWriteAuthJSONPreservesFieldOrder(t *testing.T) {
	child := &cobra.Command{Use: "login"}
	buf := captureOut(child)

	err := writeAuthJSON(child, []jsonField{
		{"success", true},
		{"authenticated", true},
		{"profile", "oauth"},
	})
	if err != nil {
		t.Fatalf("writeAuthJSON: %v", err)
	}

	out := buf.String()
	iSuccess := strings.Index(out, `"success"`)
	iAuth := strings.Index(out, `"authenticated"`)
	iProfile := strings.Index(out, `"profile"`)
	if iSuccess < 0 || iAuth < 0 || iProfile < 0 || !(iSuccess < iAuth && iAuth < iProfile) {
		t.Fatalf("fields out of order:\n%s", out)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if parsed["success"] != true || parsed["authenticated"] != true || parsed["profile"] != "oauth" {
		t.Errorf("unexpected parsed values: %+v", parsed)
	}
}

func TestWriteAuthJSONEscapesValues(t *testing.T) {
	child := &cobra.Command{Use: "login"}
	buf := captureOut(child)

	if err := writeAuthJSON(child, []jsonField{
		{"message", `contains "quotes" and \backslash`},
	}); err != nil {
		t.Fatalf("writeAuthJSON: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if parsed["message"] != `contains "quotes" and \backslash` {
		t.Errorf("message = %q, want the original string preserved", parsed["message"])
	}
}

func TestAuthFormatValidation(t *testing.T) {
	for _, tc := range []struct {
		value   string
		wantErr bool
	}{
		{"text", false},
		{"json", false},
		{"", false}, // flag default
		{"yaml", true},
		{"JSON", true},
	} {
		child := &cobra.Command{Use: "login"}
		child.Flags().String("format", "text", "")
		if tc.value != "" {
			if err := child.Flags().Set("format", tc.value); err != nil {
				t.Fatalf("set format=%q: %v", tc.value, err)
			}
		}
		_, err := authFormat(child)
		if (err != nil) != tc.wantErr {
			t.Errorf("authFormat(%q) error = %v, wantErr %v", tc.value, err, tc.wantErr)
		}
	}
}
