package archive

import "testing"

func TestParseComponentsIncludeExclude(t *testing.T) {
	c, err := ParseComponents([]string{"schema,records,attachments"}, []string{"attachments"})
	if err != nil {
		t.Fatal(err)
	}
	if !c.Schema || !c.Records || c.Attachments || c.Comments || c.Views {
		t.Fatalf("unexpected components: %+v", c)
	}
}

func TestParseComponentsRejectsUnknown(t *testing.T) {
	_, err := ParseComponents([]string{"schema,robots"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
