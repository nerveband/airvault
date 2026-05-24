package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const (
	ExitGeneral    = 1
	ExitAPI        = 2
	ExitConfig     = 3
	ExitValidation = 4
	ExitConflict   = 5
)

type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
	Retryable bool   `json:"retryable"`
	ExitCode  int    `json:"-"`
}

func (e *Error) Error() string { return e.Message }

func IsJSON(format string) bool {
	return format == "json" || (!term.IsTerminal(int(os.Stdout.Fd())) && format == "auto") || os.Getenv("AI_AGENT") != ""
}

func Write(w io.Writer, format string, value any, table func(io.Writer) error) error {
	if IsJSON(format) || format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}
	if format == "ndjson" {
		return writeNDJSON(w, value)
	}
	if table != nil {
		return table(w)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func writeNDJSON(w io.Writer, value any) error {
	switch v := value.(type) {
	case []any:
		enc := json.NewEncoder(w)
		for _, item := range v {
			if err := enc.Encode(item); err != nil {
				return err
			}
		}
		return nil
	default:
		return json.NewEncoder(w).Encode(value)
	}
}

func HandleError(err error, format string) int {
	if err == nil {
		return 0
	}
	var e *Error
	if !errors.As(err, &e) {
		e = &Error{Code: "GENERAL_ERROR", Message: err.Error(), ExitCode: ExitGeneral}
	}
	if e.ExitCode == 0 {
		e.ExitCode = ExitGeneral
	}
	payload := map[string]any{"error": e}
	if IsJSON(format) || os.Getenv("AI_AGENT") != "" {
		_ = json.NewEncoder(os.Stderr).Encode(payload)
	} else {
		msg := fmt.Sprintf("Error [%s]: %s", e.Code, e.Message)
		if strings.TrimSpace(e.Hint) != "" {
			msg += "\nHint: " + e.Hint
		}
		fmt.Fprintln(os.Stderr, msg)
	}
	return e.ExitCode
}
