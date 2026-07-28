package x

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// policy_test.go holds the build-failing tests from spec 3003 doc 06 section 5.3.
// They are not unit tests of anything. They are the promises in doc 00 turned
// into a red build, because a promise a reader has to take on trust is not worth
// much when the code is 6000 lines.
//
// They walk the shipped source, not the tests. A test file legitimately stands
// up an httptest server and pokes it, and forbidding that would only teach
// people to route around the check.

// allowedPOST is the one write this tool makes: minting an anonymous guest
// token. It writes nothing to anybody's account, it is what the x.com web
// client does before it has a session, and it is allowlisted by exact URL so
// that a second POST anywhere cannot hide behind it.
const allowedPOST = "https://api.x.com/1.1/guest/activate.json"

// No write verbs. x reads. There is no x post, x like, x follow, x dm, and the
// HTTP layer cannot express one even if a command tried.
func TestNoWriteVerbs(t *testing.T) {
	writeMethods := map[string]bool{
		"MethodPost": true, "MethodPut": true, "MethodPatch": true, "MethodDelete": true,
		"POST": true, "PUT": true, "PATCH": true, "DELETE": true,
		"Post": true, "PostForm": true, "Put": true, "Patch": true, "Delete": true,
	}
	forEachSourceFile(t, func(path string, fset *token.FileSet, f *ast.File, _ []byte) {
		allowed := allowlistedRanges(f)
		ast.Inspect(f, func(n ast.Node) bool {
			name := ""
			switch v := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := v.X.(*ast.Ident); ok && id.Name == "http" {
					name = v.Sel.Name
				}
			case *ast.BasicLit:
				if v.Kind == token.STRING {
					s, err := strconv.Unquote(v.Value)
					if err == nil {
						name = s
					}
				}
			}
			if name == "" || !writeMethods[name] {
				return true
			}
			for _, r := range allowed {
				if n.Pos() >= r[0] && n.End() <= r[1] {
					return true
				}
			}
			t.Errorf("%s: %s is a write verb, and x reads. "+
				"the only allowed write is the guest-token mint at %s",
				pos(fset, n.Pos()), name, allowedPOST)
			return true
		})
	})
}

// The command surface says the same thing out loud. A reader should not have to
// audit the HTTP layer to learn that the tool cannot post on their behalf.
func TestNoWriteCommands(t *testing.T) {
	banned := []string{"post ", "tweet-post", "reply ", "like ", "unlike ", "repost ",
		"retweet ", "follow ", "unfollow ", "dm ", "block ", "mute ", "delete "}
	forEachSourceFile(t, func(path string, fset *token.FileSet, f *ast.File, _ []byte) {
		if !strings.HasPrefix(path, "cli/") {
			return
		}
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Use" {
				return true
			}
			lit, ok := kv.Value.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			use, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, b := range banned {
				if strings.HasPrefix(use+" ", b) {
					t.Errorf("%s: command %q is a write verb, and x reads", pos(fset, kv.Pos()), use)
				}
			}
			return true
		})
	})
}

// No paid API. The whole spec hangs off this one: api.x.com/2/ is off the table,
// and so is any credential the tool asks the user to go and create. The only
// credential it ever touches is session cookies the user chooses to hand over.
func TestNoPaidAPI(t *testing.T) {
	banned := map[string]string{
		"api.x.com/2/":         "the paid v2 API",
		"api.twitter.com/2/":   "the paid v2 API",
		"X_API_KEY":            "a paid API credential",
		"X_API_SECRET":         "a paid API credential",
		"X_BEARER_TOKEN":       "a paid API credential",
		"TWITTER_API_KEY":      "a paid API credential",
		"TWITTER_API_SECRET":   "a paid API credential",
		"TWITTER_BEARER_TOKEN": "a paid API credential",
		"consumer_secret":      "a paid API credential",
	}
	forEachSourceFile(t, func(path string, fset *token.FileSet, f *ast.File, src []byte) {
		text := string(src)
		for needle, why := range banned {
			if i := strings.Index(text, needle); i >= 0 {
				t.Errorf("%s: mentions %q, which is %s. x uses the free public surfaces only",
					path+":"+lineOf(text, i), needle, why)
			}
		}
		// Reading a bearer out of the environment is the same thing wearing a
		// different hat, so it is caught by shape rather than by name.
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Getenv" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			up := strings.ToUpper(name)
			for _, bad := range []string{"BEARER", "API_KEY", "API_SECRET", "CONSUMER"} {
				if strings.Contains(up, bad) {
					t.Errorf("%s: reads %s from the environment, which is a paid-API credential",
						pos(fset, call.Pos()), name)
				}
			}
			return true
		})
	})
}

// The one hardcoded bearer is the public web one every x.com page ships to every
// visitor. It is allowlisted by being declared exactly once, next to a comment
// that says what it is, so a second hardcoded credential cannot slip in beside
// it looking like the first.
func TestOnlyOneHardcodedBearerAndItIsThePublicOne(t *testing.T) {
	const public = "AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"
	found := 0
	forEachSourceFile(t, func(path string, fset *token.FileSet, f *ast.File, src []byte) {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil || len(s) < 60 || !strings.HasPrefix(s, "AAAAAAAA") {
				return true
			}
			if s != public {
				t.Errorf("%s: a hardcoded credential that is not the public web bearer", pos(fset, lit.Pos()))
				return true
			}
			found++
			return true
		})
	})
	if found != 1 {
		t.Errorf("the public web bearer is written %d times, want exactly one declaration", found)
	}
}

// ---- walking ----

// allowlistedRanges returns the source ranges of every composite literal that
// names the one allowed POST target, so the method field inside it is exempt.
func allowlistedRanges(f *ast.File) [][2]token.Pos {
	var out [][2]token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			b, ok := kv.Value.(*ast.BasicLit)
			if !ok || b.Kind != token.STRING {
				continue
			}
			if s, err := strconv.Unquote(b.Value); err == nil && s == allowedPOST {
				out = append(out, [2]token.Pos{lit.Pos(), lit.End()})
				return true
			}
		}
		return true
	})
	return out
}

// forEachSourceFile walks every non-test .go file in the module.
func forEachSourceFile(t *testing.T, fn func(path string, fset *token.FileSet, f *ast.File, src []byte)) {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "docs", "dist", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, p, src, parser.ParseComments)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		seen++
		fn(filepath.ToSlash(rel), fset, f, src)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 10 {
		t.Fatalf("walked %d source files, which is too few to be the whole module", seen)
	}
}

func pos(fset *token.FileSet, p token.Pos) string {
	q := fset.Position(p)
	return filepath.Base(filepath.Dir(q.Filename)) + "/" + filepath.Base(q.Filename) + ":" + strconv.Itoa(q.Line)
}

func lineOf(text string, off int) string {
	return strconv.Itoa(strings.Count(text[:off], "\n") + 1)
}
