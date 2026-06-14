package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/render"
)

// x defaults to the readable list/section view on a terminal instead of kit's
// table, and to jsonl when piped; an explicit -o or --template still wins.
func TestOutDefaultFormat(t *testing.T) {
	cases := []struct {
		name string
		out  kit.OutputOptions
		want render.Format
	}{
		{"tty auto -> list", kit.OutputOptions{IsTTY: true}, render.List},
		{"piped auto -> jsonl", kit.OutputOptions{IsTTY: false}, render.JSONL},
		{"explicit table wins on tty", kit.OutputOptions{IsTTY: true, Format: "table"}, render.Table},
		{"explicit json wins when piped", kit.OutputOptions{IsTTY: false, Format: "json"}, render.JSON},
		{"template wins over list", kit.OutputOptions{IsTTY: true, Template: "{{.id}}"}, render.Template},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := &App{st: &kit.State{Output: c.out}}
			r, err := a.out()
			if err != nil {
				t.Fatal(err)
			}
			if r.Format() != c.want {
				t.Errorf("Format = %q, want %q", r.Format(), c.want)
			}
		})
	}
}

// These tests pin the rendering contract x relies on now that output goes
// through kit's shared render.Renderer (Row is render.Record): a curated
// Cols/Vals column set drives table/csv/url, and the typed Value drives
// json/jsonl/template. They guard the behavior the old hand-rolled formatter
// used to own, so a kit bump that changed it would fail here.

func mkRow() Row {
	return Row{
		Cols:  []string{"id", "author", "likes"},
		Vals:  []string{"20", "jack", "312045"},
		Value: map[string]any{"id": "20", "author": "jack", "likes": 312045},
	}
}

func newRenderer(t *testing.T, w *bytes.Buffer, o render.Options) *render.Renderer {
	t.Helper()
	o.Writer = w
	r, err := render.New(o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestEmitCSVWithFields(t *testing.T) {
	var b bytes.Buffer
	r := newRenderer(t, &b, render.Options{Format: render.CSV, Fields: []string{"id", "likes"}})
	if err := r.Emit(mkRow()); err != nil {
		t.Fatal(err)
	}
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "id,likes\n20,312045\n"
	if b.String() != want {
		t.Errorf("csv = %q, want %q", b.String(), want)
	}
}

func TestEmitJSONL(t *testing.T) {
	var b bytes.Buffer
	r := newRenderer(t, &b, render.Options{Format: render.JSONL})
	if err := r.Emit(mkRow()); err != nil {
		t.Fatal(err)
	}
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"id":"20"`) {
		t.Errorf("jsonl missing id: %q", b.String())
	}
}

func TestEmitTemplateAddsNewline(t *testing.T) {
	var b bytes.Buffer
	r := newRenderer(t, &b, render.Options{Template: "{{.author}}"})
	if err := r.Emit(mkRow()); err != nil {
		t.Fatal(err)
	}
	if err := r.Flush(); err != nil {
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
	r := newRenderer(t, &b, render.Options{Template: "{{.username}} {{.metrics.followers}}"})
	if err := r.Emit(Row{Value: prof{Username: "NASA", Metrics: metrics{Followers: 92099694}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	if b.String() != "NASA 92099694\n" {
		t.Errorf("template struct = %q, want %q", b.String(), "NASA 92099694\n")
	}
}

func TestEmitJSONArrayEmpty(t *testing.T) {
	var b bytes.Buffer
	r := newRenderer(t, &b, render.Options{Format: render.JSON})
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(b.String()) != "[]" {
		t.Errorf("empty json = %q", b.String())
	}
}

// A tweet row keeps the curated table columns and carries the typed tweet as
// Value, so json sees the full object while the table stays compact.
func TestTweetRowColumns(t *testing.T) {
	var b bytes.Buffer
	r := newRenderer(t, &b, render.Options{Format: render.CSV, NoHeader: true})
	row := Row{
		Cols:  []string{"id", "author", "text"},
		Vals:  []string{"20", "jack", "just setting up my twttr"},
		Value: map[string]any{"id": "20"},
	}
	if err := r.Emit(row); err != nil {
		t.Fatal(err)
	}
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); !strings.HasPrefix(got, "20,jack,") {
		t.Errorf("csv row = %q", got)
	}
}
