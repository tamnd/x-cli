package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"text/template"
)

// Row is one renderable record: a curated column set for the table/csv views
// plus the full typed object as Value for --json and --template (spec §4.3/§4.4).
type Row struct {
	Cols  []string
	Vals  []string
	Value any
}

// Output renders Rows in the chosen format, streaming where possible. Snowflake
// IDs are always strings (the Value objects tag them as such).
type Output struct {
	w        io.Writer
	format   string
	fields   []string
	tmpl     *template.Template
	noHeader bool

	started bool
	tw      *tabwriter.Writer
	cw      *csv.Writer
	enc     *json.Encoder
	first   bool
	header  []string
}

// NewOutput builds an Output for a format. tmplStr, when non-empty, switches to
// template rendering over each Row.Value.
func NewOutput(w io.Writer, format, fields, tmplStr string, noHeader bool) (*Output, error) {
	o := &Output{w: w, format: format, noHeader: noHeader, first: true}
	if fields != "" {
		o.fields = splitComma(fields)
	}
	if tmplStr != "" {
		t, err := template.New("row").Parse(tmplStr)
		if err != nil {
			return nil, fmt.Errorf("bad --template: %w", err)
		}
		o.tmpl = t
		o.format = "template"
	}
	return o, nil
}

func splitComma(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Emit renders one row.
func (o *Output) Emit(r Row) error {
	switch o.format {
	case "json":
		return o.emitJSONArray(r)
	case "jsonl":
		return o.emitJSONL(r)
	case "csv", "tsv":
		return o.emitDelim(r)
	case "url":
		return o.emitURL(r)
	case "raw":
		return o.emitRaw(r)
	case "template":
		if err := o.tmpl.Execute(o.w, templateData(r.Value)); err != nil {
			return err
		}
		_, err := io.WriteString(o.w, "\n")
		return err
	default: // table
		return o.emitTable(r)
	}
}

// templateData makes --template see the same json-tag keys as --json/--jsonl.
// A row whose Value is a typed struct is round-tripped to a generic map so a
// template can write {{.id}} or {{.author}} regardless of Go field names; a
// value that is already a map (or fails to convert) is passed through.
func templateData(v any) any {
	switch v.(type) {
	case map[string]any, nil:
		return v
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber() // keep ints as digits, not 9.2e+07
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return v
	}
	return m
}

func (o *Output) project(r Row) (cols, vals []string) {
	if len(o.fields) == 0 {
		return r.Cols, r.Vals
	}
	idx := map[string]int{}
	for i, c := range r.Cols {
		idx[c] = i
	}
	for _, f := range o.fields {
		cols = append(cols, f)
		if i, ok := idx[f]; ok && i < len(r.Vals) {
			vals = append(vals, r.Vals[i])
		} else {
			vals = append(vals, "")
		}
	}
	return cols, vals
}

func (o *Output) emitTable(r Row) error {
	if o.tw == nil {
		o.tw = tabwriter.NewWriter(o.w, 0, 2, 2, ' ', 0)
	}
	cols, vals := o.project(r)
	if !o.started {
		o.started = true
		if !o.noHeader {
			fmt.Fprintln(o.tw, strings.Join(upper(cols), "\t"))
		}
	}
	fmt.Fprintln(o.tw, strings.Join(clean(vals), "\t"))
	return nil
}

func (o *Output) emitJSONArray(r Row) error {
	if !o.started {
		o.started = true
		io.WriteString(o.w, "[\n")
	}
	if !o.first {
		io.WriteString(o.w, ",\n")
	}
	o.first = false
	b, err := json.MarshalIndent(r.Value, "  ", "  ")
	if err != nil {
		return err
	}
	io.WriteString(o.w, "  ")
	_, err = o.w.Write(b)
	return err
}

func (o *Output) emitJSONL(r Row) error {
	if o.enc == nil {
		o.enc = json.NewEncoder(o.w)
	}
	return o.enc.Encode(r.Value)
}

func (o *Output) emitDelim(r Row) error {
	if o.cw == nil {
		o.cw = csv.NewWriter(o.w)
		if o.format == "tsv" {
			o.cw.Comma = '\t'
		}
	}
	cols, vals := o.project(r)
	if !o.started {
		o.started = true
		if !o.noHeader {
			if err := o.cw.Write(cols); err != nil {
				return err
			}
		}
	}
	return o.cw.Write(vals)
}

func (o *Output) emitURL(r Row) error {
	cols, vals := r.Cols, r.Vals
	for i, c := range cols {
		if c == "url" && i < len(vals) && vals[i] != "" {
			_, err := fmt.Fprintln(o.w, vals[i])
			return err
		}
	}
	return nil
}

func (o *Output) emitRaw(r Row) error {
	if s, ok := r.Value.(string); ok {
		_, err := fmt.Fprintln(o.w, s)
		return err
	}
	b, err := json.Marshal(r.Value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(o.w, string(b))
	return err
}

// Flush closes any open array/buffered writer.
func (o *Output) Flush() error {
	switch {
	case o.tw != nil:
		return o.tw.Flush()
	case o.cw != nil:
		o.cw.Flush()
		return o.cw.Error()
	case o.format == "json":
		if !o.started {
			io.WriteString(o.w, "[]\n")
		} else {
			io.WriteString(o.w, "\n]\n")
		}
	}
	return nil
}

func upper(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToUpper(s)
	}
	return out
}

func clean(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\t", " ")
	}
	return out
}
