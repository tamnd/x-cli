package embed

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSerovalLiterals(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want any
	}{
		{"string", `$_TSR.router=($R=>"hi")`, "hi"},
		{"int", `$_TSR.router=($R=>42)`, int64(42)},
		{"negative", `$_TSR.router=($R=>-7)`, int64(-7)},
		{"float", `$_TSR.router=($R=>1.5)`, 1.5},
		{"true", `$_TSR.router=($R=>!0)`, true},
		{"false", `$_TSR.router=($R=>!1)`, false},
		{"undefined", `$_TSR.router=($R=>void 0)`, nil},
		{"null", `$_TSR.router=($R=>null)`, nil},
		{"empty array", `$_TSR.router=($R=>[])`, []any{}},
		{"array", `$_TSR.router=($R=>[1,"a",!0])`, []any{int64(1), "a", true}},
		{"bare key", `$_TSR.router=($R=>{a:1})`, map[string]any{"a": int64(1)}},
		{"quoted key", `$_TSR.router=($R=>{"a b":1})`, map[string]any{"a b": int64(1)}},
		{"escapes", `$_TSR.router=($R=>"a\nb\tc\x00dA")`, "a\nb\tc\x00dA"},
		{"surrogate pair", `$_TSR.router=($R=>"🚀")`, "\U0001F680"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := Seroval(c.in)
			if err != nil {
				t.Fatalf("Seroval: %v", err)
			}
			if !reflect.DeepEqual(d.Router, c.want) {
				t.Errorf("got %#v, want %#v", d.Router, c.want)
			}
		})
	}
}

// A register both defines and returns, so the same object has to come back
// from every later reference to it. Sharing is the whole point of the
// encoding: the Relay store leans on it heavily.
func TestSerovalRegisterIsShared(t *testing.T) {
	d, err := Seroval(`$_TSR.router=($R=>{one:$R[3]={n:1},two:$R[3]})`)
	if err != nil {
		t.Fatal(err)
	}
	m := d.Router.(map[string]any)
	one := m["one"].(map[string]any)
	two := m["two"].(map[string]any)
	one["n"] = int64(99)
	if two["n"] != int64(99) {
		t.Fatalf("registers are not shared: two[n] = %v", two["n"])
	}
}

func TestSerovalNoPayload(t *testing.T) {
	_, err := Seroval("<html><body>nothing here</body></html>")
	if !errors.Is(err, ErrNoPayload) {
		t.Fatalf("want ErrNoPayload, got %v", err)
	}
}

// Functions are skipped, not evaluated, and not silently turned into null.
// A caller has to be able to tell "this field was code" from "this field was
// empty", so the marker is a distinct type.
func TestSerovalOpaque(t *testing.T) {
	d, err := Seroval(`$_TSR.router=($R=>{fn:$R[1]=e=>e+1,after:2})`)
	if err != nil {
		t.Fatal(err)
	}
	m := d.Router.(map[string]any)
	if _, ok := m["fn"].(Opaque); !ok {
		t.Errorf("fn = %#v, want Opaque", m["fn"])
	}
	if m["after"] != int64(2) {
		t.Errorf("parse did not resume after the function: after = %v", m["after"])
	}
}

// The ReadableStream wrapper in a real page is an IIFE with a nested register
// assignment, and the stream register is referenced from a later script tag.
// If skipping the IIFE loses the register, that later reference dangles.
func TestSerovalIIFERegistersSurvive(t *testing.T) {
	const in = `$_TSR.router=($R=>{s:$R[9]=($R[10]=e=>e)($R[11]=()=>1)})` +
		`</script><script>($R=>$R[11].next({v:5}))($R["tsr"])`
	d, err := Seroval(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(d.Chunks))
	}
	want := map[string]any{"v": int64(5)}
	if !reflect.DeepEqual(d.Chunks[0], want) {
		t.Errorf("chunk = %#v, want %#v", d.Chunks[0], want)
	}
}

// A back-reference to a register nobody defined means the encoding changed
// under us, which is worth failing on rather than filling in with a nil.
func TestSerovalUndefinedRegister(t *testing.T) {
	_, err := Seroval(`$_TSR.router=($R=>{a:$R[7]})`)
	if err == nil || !strings.Contains(err.Error(), "before it was defined") {
		t.Fatalf("want an undefined-register error, got %v", err)
	}
}

// The real thing. A status page and two profile pages, captured live on
// 2026-07-28, each parsed end to end with no Opaque in the data we care about.
func TestSerovalRealPages(t *testing.T) {
	cases := []struct {
		file       string
		wantChunks int
	}{
		{"sp2.html.gz", 0},
		{"prof.html.gz", 1},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			d, err := Seroval(fixture(t, c.file))
			if err != nil {
				t.Fatalf("Seroval: %v", err)
			}
			m, ok := d.Router.(map[string]any)
			if !ok {
				t.Fatalf("router is %T, want an object", d.Router)
			}
			if len(m) == 0 {
				t.Fatal("router object is empty")
			}
			if len(d.Chunks) != c.wantChunks {
				t.Errorf("got %d streamed chunks, want %d", len(d.Chunks), c.wantChunks)
			}
		})
	}
}

// The status page carries the whole Relay store for the tweet, so the walk in
// relay.go has something to find. This asserts the shape the rest of the tool
// depends on rather than any particular count.
func TestSerovalStatusPageHasRelayRecords(t *testing.T) {
	d, err := Seroval(fixture(t, "sp2.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	recs := findRelayRecords(d)
	if len(recs) == 0 {
		t.Fatal("no relayRecords in the status page")
	}
	root, ok := recs["client:root"].(map[string]any)
	if !ok {
		t.Fatalf("no client:root record, got %d records", len(recs))
	}
	if root["__typename"] != "__Root" {
		t.Errorf("client:root __typename = %v, want __Root", root["__typename"])
	}
}
