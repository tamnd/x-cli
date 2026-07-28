// Package embed pulls structured data out of the HTML that X serves.
//
// Four planes live here, all documented in spec 3003 doc 02:
//
//	__NEXT_DATA__ on syndication.twitter.com, which is plain JSON
//	the seroval-encoded Relay store on x.com, which is a JS expression
//	schema.org JSON-LD, which arrives inside that Relay payload
//	schema.org microdata and OpenGraph meta, which are in the DOM
//
// Nothing outside this package knows what a script tag is.
package embed

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// ErrNoPayload means the document did not carry the plane we were asked for.
// It is deliberately distinct from "the payload was empty": a caller must be
// able to tell "X changed the page" from "this user has no tweets".
var ErrNoPayload = errors.New("no payload in document")

// Opaque stands in for a value we deliberately did not model: an arrow
// function, an IIFE, a ReadableStream wrapper. The parser records that the
// value was there and moves on rather than guessing at its contents.
//
// It is a distinct type on purpose. A caller that reaches an Opaque knows it
// hit real JavaScript, which is a different thing from a null.
type Opaque struct{}

// Doc is one x.com page's worth of seroval payload.
//
// The page ships the store in two halves. The router payload comes down in the
// first script tag, and any records the server had not finished rendering
// arrive later as $R[n].next(...) pushes into a seroval stream. They share one
// register array, so they have to be parsed together or the back-references in
// the later chunks dangle.
type Doc struct {
	// Router is the value of $_TSR.router.
	Router any
	// Chunks are the values pushed into a stream by later script tags, in
	// document order.
	Chunks []any
}

// serovalScript is how every hydration script tag opens. The router one is
// prefixed with $_TSR.router=; the stream pushes are bare.
const serovalScript = `($R=>`

// routerAnchor marks the first and largest payload.
const routerAnchor = `$_TSR.router=($R=>`

// Seroval reads the x.com hydration payload out of an HTML document.
//
// The payload is not JSON. It is a JavaScript expression using seroval's
// register encoding: unquoted identifier keys, !0 and !1 for booleans, void 0
// for undefined, and $R[n]= to define-and-return a value that later $R[n]
// lookups share.
//
// This is a parser, not an evaluator. It reads the data subset seroval emits
// for plain values, and for anything else, meaning real code, it skips the
// expression and leaves an Opaque behind. It never runs anything.
func Seroval(html string) (*Doc, error) {
	if !strings.Contains(html, routerAnchor) {
		return nil, fmt.Errorf("%w: no %s", ErrNoPayload, routerAnchor)
	}
	p := &serovalParser{s: html, reg: map[string]any{}}
	doc := &Doc{}
	var sawRouter bool

	// Every hydration script opens the same way and they all share p.reg, so
	// walk them in document order.
	for at := 0; ; {
		i := strings.Index(html[at:], serovalScript)
		if i < 0 {
			break
		}
		p.i = at + i + len(serovalScript)
		at = p.i
		isRouter := strings.HasSuffix(html[:at], routerAnchor)

		v, err := p.value()
		if err != nil {
			return nil, fmt.Errorf("seroval at byte %d: %w", p.i, err)
		}
		if isRouter {
			doc.Router, sawRouter = v, true
		}
		at = p.i
	}
	doc.Chunks = p.chunks
	if !sawRouter {
		return nil, fmt.Errorf("%w: router payload did not parse", ErrNoPayload)
	}
	return doc, nil
}

type serovalParser struct {
	s      string
	i      int
	reg    map[string]any
	chunks []any
}

func (p *serovalParser) value() (any, error) {
	p.space()
	if p.i >= len(p.s) {
		return nil, errors.New("unexpected end of input")
	}
	var (
		v   any
		err error
	)
	switch c := p.s[p.i]; {
	case c == '{':
		v, err = p.object()
	case c == '[':
		v, err = p.array()
	case c == '"':
		v, err = p.str()
	case c == '$' && strings.HasPrefix(p.s[p.i:], "$R["):
		v, err = p.register()
	case c == '!' && p.i+1 < len(p.s) && (p.s[p.i+1] == '0' || p.s[p.i+1] == '1'):
		// !0 is true, !1 is false. Nothing else uses ! in this encoding.
		v = p.s[p.i+1] == '0'
		p.i += 2
	case strings.HasPrefix(p.s[p.i:], "void 0"):
		p.i += len("void 0")
	case strings.HasPrefix(p.s[p.i:], "null"):
		p.i += 4
	case strings.HasPrefix(p.s[p.i:], "undefined"):
		p.i += len("undefined")
	case c == '-' || c >= '0' && c <= '9':
		v, err = p.number()
	default:
		// Real code: an arrow function, an IIFE, a new expression. Skip it.
		p.skipExpr()
		v = Opaque{}
	}
	if err != nil {
		return nil, err
	}
	return p.postfix(v)
}

// postfix handles the stream pushes that follow a register lookup, which is
// the only method call this encoding uses: $R[n].next(v), .return(v), .throw(v).
func (p *serovalParser) postfix(v any) (any, error) {
	for {
		p.space()
		rest := p.s[p.i:]
		var name string
		switch {
		case strings.HasPrefix(rest, ".next("):
			name = "next"
		case strings.HasPrefix(rest, ".return("):
			name = "return"
		case strings.HasPrefix(rest, ".throw("):
			name = "throw"
		default:
			return v, nil
		}
		p.i += len(name) + 2
		arg, err := p.value()
		if err != nil {
			return nil, fmt.Errorf(".%s argument: %w", name, err)
		}
		p.space()
		if p.i < len(p.s) && p.s[p.i] == ')' {
			p.i++
		}
		// A return closes the stream and carries nothing worth keeping; a
		// throw carries an error we have no use for. Only next() has records.
		if name == "next" && arg != nil {
			p.chunks = append(p.chunks, arg)
		}
		v = Opaque{}
	}
}

// register handles both $R[n]=<value>, which defines, and $R[n], which looks
// up. The index is usually a number but the page also uses $R["tsr"].
func (p *serovalParser) register() (any, error) {
	p.i += 3 // past $R[
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != ']' {
		p.i++
	}
	if p.i >= len(p.s) {
		return nil, errors.New("unterminated $R index")
	}
	key := strings.Trim(p.s[start:p.i], `"`)
	p.i++ // past ]
	p.space()
	if p.i < len(p.s) && p.s[p.i] == '=' && (p.i+1 >= len(p.s) || p.s[p.i+1] != '=') {
		p.i++
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		p.reg[key] = v
		return v, nil
	}
	v, ok := p.reg[key]
	if !ok {
		// Forward references do not occur in seroval output, so this is a
		// shape change rather than something to paper over.
		return nil, fmt.Errorf("$R[%s] used before it was defined", key)
	}
	return v, nil
}

func (p *serovalParser) object() (any, error) {
	p.i++ // past {
	out := map[string]any{}
	for {
		p.space()
		if p.i >= len(p.s) {
			return nil, errors.New("unterminated object")
		}
		if p.s[p.i] == '}' {
			p.i++
			return out, nil
		}
		key, err := p.key()
		if err != nil {
			return nil, err
		}
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, fmt.Errorf("expected : after key %q", key)
		}
		p.i++
		v, err := p.value()
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", key, err)
		}
		out[key] = v
		p.space()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
		}
	}
}

func (p *serovalParser) array() (any, error) {
	p.i++ // past [
	out := []any{}
	for {
		p.space()
		if p.i >= len(p.s) {
			return nil, errors.New("unterminated array")
		}
		if p.s[p.i] == ']' {
			p.i++
			return out, nil
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.space()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
		}
	}
}

// key reads either a quoted key or a bare JavaScript identifier. Relay root
// field names are whole GraphQL fields with their arguments inlined, so those
// always arrive quoted.
func (p *serovalParser) key() (string, error) {
	if p.s[p.i] == '"' {
		v, err := p.str()
		if err != nil {
			return "", err
		}
		return v.(string), nil
	}
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			p.i++
			continue
		}
		break
	}
	if start == p.i {
		return "", fmt.Errorf("expected key, got %q", p.s[p.i])
	}
	return p.s[start:p.i], nil
}

func (p *serovalParser) str() (any, error) {
	p.i++ // past opening quote
	var b strings.Builder
	for p.i < len(p.s) {
		switch c := p.s[p.i]; c {
		case '"':
			p.i++
			return b.String(), nil
		case '\\':
			p.i++
			if p.i >= len(p.s) {
				return nil, errors.New("unterminated escape")
			}
			switch e := p.s[p.i]; e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'v':
				b.WriteByte('\v')
			case '0':
				b.WriteByte(0)
			case 'u':
				r, n, err := p.unicodeEscape()
				if err != nil {
					return nil, err
				}
				b.WriteRune(r)
				p.i += n
			case 'x':
				if p.i+2 >= len(p.s) {
					return nil, errors.New(`bad \x escape`)
				}
				n, err := strconv.ParseUint(p.s[p.i+1:p.i+3], 16, 8)
				if err != nil {
					return nil, fmt.Errorf(`bad \x escape: %w`, err)
				}
				b.WriteRune(rune(n))
				p.i += 2
			default:
				b.WriteByte(e)
			}
			p.i++
		default:
			b.WriteByte(c)
			p.i++
		}
	}
	return nil, errors.New("unterminated string")
}

// unicodeEscape decodes \uXXXX at p.i, pairing surrogates where they appear.
// It returns the rune and how many bytes past the u were consumed.
func (p *serovalParser) unicodeEscape() (rune, int, error) {
	if p.i+4 >= len(p.s) {
		return 0, 0, errors.New(`truncated \u escape`)
	}
	n, err := strconv.ParseUint(p.s[p.i+1:p.i+5], 16, 32)
	if err != nil {
		return 0, 0, fmt.Errorf(`bad \u escape: %w`, err)
	}
	r := rune(n)
	if utf16.IsSurrogate(r) && p.i+11 < len(p.s) && p.s[p.i+5] == '\\' && p.s[p.i+6] == 'u' {
		if lo, err := strconv.ParseUint(p.s[p.i+7:p.i+11], 16, 32); err == nil {
			if dec := utf16.DecodeRune(r, rune(lo)); dec != 0xFFFD {
				return dec, 10, nil
			}
		}
	}
	return r, 4, nil
}

func (p *serovalParser) number() (any, error) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c >= '0' && c <= '9' || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-' {
			p.i++
			continue
		}
		break
	}
	lit := p.s[start:p.i]
	if n, err := strconv.ParseInt(lit, 10, 64); err == nil {
		return n, nil
	}
	f, err := strconv.ParseFloat(lit, 64)
	if err != nil {
		return nil, fmt.Errorf("bad number %q", lit)
	}
	return f, nil
}

// skipExpr walks past one JavaScript expression without evaluating it, so that
// a function body in the middle of the payload does not stop the parse.
//
// It tracks bracket depth and string state, and stops at the first separator
// that belongs to the enclosing structure. Register assignments found on the
// way are recorded as Opaque so that a later $R[n] lookup resolves to
// something honest instead of failing.
func (p *serovalParser) skipExpr() {
	depth := 0
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch c {
		case '"', '\'', '`':
			p.skipString(c)
			continue
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return
			}
			depth--
			// An IIFE is (fn)(args), and a chained call is (fn)(a)(b), so a
			// closing bracket is not the end if a call follows it.
			if depth == 0 {
				j := p.i + 1
				for j < len(p.s) && (p.s[j] == ' ' || p.s[j] == '\n') {
					j++
				}
				if j < len(p.s) && (p.s[j] == '(' || p.s[j] == '.' || p.s[j] == '[') {
					p.i = j
					continue
				}
			}
		case ',':
			if depth == 0 {
				return
			}
		case '$':
			if strings.HasPrefix(p.s[p.i:], "$R[") {
				p.noteRegister()
				continue
			}
		}
		p.i++
	}
}

// noteRegister records a $R[n]= assignment seen inside skipped code. The value
// is not modelled, but the register has to exist or a later back-reference to
// it reads as a shape change when it is only a function we chose not to run.
func (p *serovalParser) noteRegister() {
	j := p.i + 3
	start := j
	for j < len(p.s) && p.s[j] != ']' {
		j++
	}
	if j >= len(p.s) {
		p.i = len(p.s)
		return
	}
	key := strings.Trim(p.s[start:j], `"`)
	j++
	if j < len(p.s) && p.s[j] == '=' && (j+1 >= len(p.s) || p.s[j+1] != '=') {
		if _, seen := p.reg[key]; !seen {
			p.reg[key] = Opaque{}
		}
	}
	p.i = j
}

// skipString walks past a string literal of the given quote style.
func (p *serovalParser) skipString(quote byte) {
	p.i++
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case '\\':
			p.i += 2
			continue
		case quote:
			p.i++
			return
		}
		p.i++
	}
}

func (p *serovalParser) space() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}
