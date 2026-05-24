package archive

import (
	"fmt"
	"strings"
)

type Components struct {
	Schema      bool `json:"schema"`
	Records     bool `json:"records"`
	Attachments bool `json:"attachments"`
	Comments    bool `json:"comments"`
	Views       bool `json:"views"`
}

type Selection struct {
	Components         Components `json:"components"`
	BaseIDs            []string   `json:"base_ids,omitempty"`
	TableIDsOrNames    []string   `json:"tables,omitempty"`
	MaxAttachmentBytes int64      `json:"max_attachment_bytes,omitempty"`
	Since              string     `json:"since,omitempty"`
}

func DefaultComponents() Components {
	return Components{Schema: true, Records: true, Attachments: true, Comments: false, Views: true}
}

func ParseComponents(include, exclude []string) (Components, error) {
	c := DefaultComponents()
	if len(include) > 0 {
		c = Components{}
		for _, item := range splitValues(include) {
			if err := setComponent(&c, item, true); err != nil {
				return c, err
			}
		}
	}
	for _, item := range splitValues(exclude) {
		if err := setComponent(&c, item, false); err != nil {
			return c, err
		}
	}
	return c, nil
}

func (s Selection) IncludesTable(table TableSummary, tableName string) bool {
	if len(s.TableIDsOrNames) == 0 {
		return true
	}
	for _, item := range s.TableIDsOrNames {
		if item == table.ID || item == tableName {
			return true
		}
	}
	return false
}

func splitValues(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(strings.ToLower(part))
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func setComponent(c *Components, name string, enabled bool) error {
	switch name {
	case "all":
		c.Schema, c.Records, c.Attachments, c.Comments, c.Views = enabled, enabled, enabled, enabled, enabled
	case "schema":
		c.Schema = enabled
	case "records":
		c.Records = enabled
	case "attachments":
		c.Attachments = enabled
	case "comments":
		c.Comments = enabled
	case "views":
		c.Views = enabled
	default:
		return fmt.Errorf("unknown component %q; supported: schema,records,attachments,comments,views,all", name)
	}
	return nil
}
