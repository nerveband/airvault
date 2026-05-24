package grist

import (
	"regexp"
	"strings"
)

type FormulaTranslation struct {
	Source   string   `json:"source"`
	Output   string   `json:"output"`
	Status   string   `json:"status"`
	Warnings []string `json:"warnings,omitempty"`
}

var fieldRefPattern = regexp.MustCompile(`\{([^}]+)\}`)

func TranslateFormula(source string, fieldIDsByName map[string]string) FormulaTranslation {
	source = strings.TrimSpace(source)
	result := FormulaTranslation{Source: source, Output: source, Status: "translated"}
	if source == "" {
		result.Status = "empty"
		return result
	}
	out := fieldRefPattern.ReplaceAllStringFunc(source, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "{"), "}")
		if id := fieldIDsByName[name]; id != "" {
			return "$" + id
		}
		result.Warnings = append(result.Warnings, "unknown field reference: "+name)
		return match
	})
	out = replaceFunctionNames(out)
	if strings.Contains(out, "&") {
		out = strings.ReplaceAll(out, "&", "+")
		result.Warnings = append(result.Warnings, "converted Airtable string concatenation operator '&' to Python '+'")
	}
	for _, fn := range []string{"ARRAYJOIN", "ARRAYUNIQUE", "BLANK", "DATETIME_DIFF", "DATESTR", "IS_SAME", "RECORD_ID", "SET_TIMEZONE"} {
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(fn) + `\s*\(`).MatchString(source) {
			result.Warnings = append(result.Warnings, "Airtable function may need manual rewrite in Grist: "+fn)
		}
	}
	if len(result.Warnings) > 0 {
		result.Status = "needs_review"
	}
	result.Output = out
	return result
}

func replaceFunctionNames(input string) string {
	for _, fn := range []string{"IF", "AND", "OR", "NOT", "ROUND", "LOWER", "UPPER", "LEN", "ABS", "MAX", "MIN", "SUM"} {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(fn) + `\s*\(`)
		input = re.ReplaceAllString(input, fn+"(")
	}
	return input
}
