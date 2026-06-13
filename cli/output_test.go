package cli

import (
	"bytes"
	"strings"
	"testing"
)

func mkRow() Row {
	return Row{
		Cols:  []string{"id", "author", "likes"},
		Vals:  []string{"20", "jack", "312045"},
		Value: map[string]any{"id": "20", "author": "jack", "likes": 312045},
	}
}

func TestEmitCSVWithFields(t *testing.T) {
	var b bytes.Buffer
	o, err := NewOutput(&b, "csv", "id,likes", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Emit(mkRow()); err != nil {
		t.Fatal(err)
	}
	if err := o.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "id,likes\n20,312045\n"
	if b.String() != want {
		t.Errorf("csv = %q, want %q", b.String(), want)
	}
}

func TestEmitJSONL(t *testing.T) {
	var b bytes.Buffer
	o, _ := NewOutput(&b, "jsonl", "", "", false)
	if err := o.Emit(mkRow()); err != nil {
		t.Fatal(err)
	}
	if err := o.Flush(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"id":"20"`) {
		t.Errorf("jsonl missing id: %q", b.String())
	}
}

func TestEmitTemplateAddsNewline(t *testing.T) {
	var b bytes.Buffer
	o, err := NewOutput(&b, "", "", "{{.author}}", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Emit(mkRow()); err != nil {
		t.Fatal(err)
	}
	if err := o.Flush(); err != nil {
		t.Fatal(err)
	}
	if b.String() != "jack\n" {
		t.Errorf("template = %q, want %q", b.String(), "jack\n")
	}
}

// A typed struct value must render with json-tag keys and integer counters,
// not Go field names or float scientific notation.
func TestEmitTemplateStructValue(t *testing.T) {
	type metrics struct {
		Followers int `json:"followers"`
	}
	type prof struct {
		Username string  `json:"username"`
		Metrics  metrics `json:"metrics"`
	}
	var b bytes.Buffer
	o, err := NewOutput(&b, "", "", "{{.username}} {{.metrics.followers}}", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Emit(Row{Value: prof{Username: "NASA", Metrics: metrics{Followers: 92099694}}}); err != nil {
		t.Fatal(err)
	}
	if err := o.Flush(); err != nil {
		t.Fatal(err)
	}
	if b.String() != "NASA 92099694\n" {
		t.Errorf("template struct = %q, want %q", b.String(), "NASA 92099694\n")
	}
}

func TestEmitJSONArrayEmpty(t *testing.T) {
	var b bytes.Buffer
	o, _ := NewOutput(&b, "json", "", "", false)
	if err := o.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(b.String()) != "[]" {
		t.Errorf("empty json = %q", b.String())
	}
}
