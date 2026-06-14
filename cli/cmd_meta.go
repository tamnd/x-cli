package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/x-cli/x"
)

// metaCommands returns the session, config, cache, and convenience commands.
func metaCommands() []kit.Command {
	return []kit.Command{
		newAuthCmd(),
		newConfigCmd(),
		newCacheCmd(),
		newOpenCmd(),
		newDownloadCmd(),
		newInfoCmd(),
		newVersionCmd(),
	}
}

func newAuthCmd() kit.Command {
	var authToken, ct0, handle string
	return kit.Command{
		Use:   "auth",
		Short: "Manage your X session (Tier 2)",
		Sub: []kit.Command{
			{
				Use:   "import",
				Short: "Save your auth_token + ct0 cookies (or paste a Cookie header on stdin)",
				Write: true,
				Flags: func(f *kit.FlagSet) {
					f.StringVar(&authToken, "auth-token", "", "the auth_token cookie")
					f.StringVar(&ct0, "ct0", "", "the ct0 cookie")
					f.StringVar(&handle, "handle", "", "your @handle (label only)")
				},
				Run: func(ctx context.Context, args []string) error {
					a := appFromCtx(ctx)
					if authToken == "" || ct0 == "" {
						at, c0 := parseCookieHeader(readStdin())
						if authToken == "" {
							authToken = at
						}
						if ct0 == "" {
							ct0 = c0
						}
					}
					if authToken == "" || ct0 == "" {
						return fmt.Errorf("need both --auth-token and --ct0 (or paste your Cookie header on stdin)")
					}
					if err := x.SaveSession(x.Creds{AuthToken: authToken, CT0: ct0, Handle: handle}); err != nil {
						return err
					}
					a.logf("session saved to %s", x.SessionStorePath())
					return nil
				},
			},
			{
				Use:   "status",
				Short: "Show the current session and tier",
				Run: func(ctx context.Context, args []string) error {
					a := appFromCtx(ctx)
					cfg := a.config()
					kv := map[string]string{
						"session":     yn(cfg.HasSession()),
						"guest":       yn(cfg.AllowGuest),
						"forced_tier": orNone(cfg.Tier),
						"store":       x.SessionStorePath(),
					}
					if cr, ok := x.LoadSession(); ok && cr.Handle != "" {
						kv["handle"] = "@" + cr.Handle
					}
					return a.printKVString(kv)
				},
			},
			{
				Use:   "logout",
				Short: "Forget the saved session",
				Write: true,
				Run: func(ctx context.Context, args []string) error {
					a := appFromCtx(ctx)
					if err := x.ForgetSession(); err != nil {
						return err
					}
					a.logf("session forgotten")
					return nil
				},
			},
		},
	}
}

func newConfigCmd() kit.Command {
	return kit.Command{
		Use:   "config",
		Short: "Show config paths and resolved values",
		Sub: []kit.Command{
			{
				Use:   "path",
				Short: "Print the config file path",
				Run: func(ctx context.Context, args []string) error {
					_, err := fmt.Fprintln(os.Stdout, x.ConfigPath())
					return err
				},
			},
			{
				Use:   "show",
				Short: "Print the resolved configuration",
				Run: func(ctx context.Context, args []string) error {
					a := appFromCtx(ctx)
					cfg := a.config()
					return a.printKVString(map[string]string{
						"config_path": x.ConfigPath(),
						"data_dir":    cfg.DataDir,
						"cache_dir":   cfg.CacheDir,
						"store":       a.StorePath(),
						"session":     yn(cfg.HasSession()),
						"guest":       yn(cfg.AllowGuest),
						"forced_tier": orNone(cfg.Tier),
						"rate":        cfg.Rate.String(),
						"retries":     fmt.Sprintf("%d", cfg.Retries),
						"timeout":     cfg.Timeout.String(),
					})
				},
			},
		},
	}
}

func newCacheCmd() kit.Command {
	return kit.Command{
		Use:   "cache",
		Short: "Inspect or clear the HTTP cache",
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			cache := a.engine().Client().Cache()
			bytes, files := cache.Size()
			return a.printKVString(map[string]string{
				"dir":   a.config().CacheDir,
				"files": fmt.Sprintf("%d", files),
				"bytes": fmt.Sprintf("%d", bytes),
			})
		},
		Sub: []kit.Command{
			{
				Use:   "clear",
				Short: "Delete all cached responses",
				Write: true,
				Run: func(ctx context.Context, args []string) error {
					a := appFromCtx(ctx)
					if err := a.engine().Client().Cache().Clear(); err != nil {
						return err
					}
					a.logf("cache cleared")
					return nil
				},
			},
		},
	}
}

func newOpenCmd() kit.Command {
	return kit.Command{
		Use:   "open <ref>",
		Short: "Open a tweet or profile in your browser",
		Args:  kit.ExactArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			url := refURL(args[0])
			if a.dryRun {
				_, err := fmt.Fprintln(os.Stdout, url)
				return err
			}
			return openBrowser(url)
		},
	}
}

func newDownloadCmd() kit.Command {
	var outDir string
	return kit.Command{
		Use:     "download <ref>",
		Aliases: []string{"dl"},
		Short:   "Download a tweet's media to disk",
		Args:    kit.ExactArgs(1),
		Write:   true,
		Flags: func(f *kit.FlagSet) {
			f.StringVarP(&outDir, "out", "O", ".", "output directory")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			t, err := a.engine().Tweet(a.ctx(), id)
			if err != nil {
				return mapErr(err)
			}
			if len(t.Media) == 0 {
				return fmt.Errorf("tweet %s has no media", id)
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return err
			}
			out, err := a.out()
			if err != nil {
				return err
			}
			n := 0
			for i, m := range t.Media {
				u := bestMediaURL(m)
				if u == "" {
					continue
				}
				name := fmt.Sprintf("%s-%d%s", id, i+1, extOf(u))
				dst := filepath.Join(outDir, name)
				if err := downloadFile(a.ctx(), u, dst); err != nil {
					a.logf("warn: %s: %v", name, err)
					continue
				}
				n++
				_ = out.Emit(Row{Cols: []string{"file", "type", "url"}, Vals: []string{dst, m.Type, u},
					Value: map[string]any{"file": dst, "type": m.Type, "url": u}})
			}
			if e := out.Flush(); e != nil {
				return e
			}
			if n == 0 {
				return mapErr(errNoResults)
			}
			return nil
		},
	}
}

func newInfoCmd() kit.Command {
	return kit.Command{
		Use:   "info",
		Short: "Show resolved tiers and capabilities",
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			cfg := a.config()
			gql := cfg.HasSession() || cfg.AllowGuest || cfg.Tier == "guest" || cfg.Tier == "session"
			caps := map[string]string{
				"tier0_syndication": "yes (no auth)",
				"tier1_guest":       yn(cfg.AllowGuest || cfg.Tier == "guest"),
				"tier2_session":     yn(cfg.HasSession()),
				"search":            yn(gql),
				"followers/likes":   yn(gql),
				"home/bookmarks":    yn(cfg.HasSession()),
			}
			return a.printKVString(caps)
		},
	}
}

func newVersionCmd() kit.Command {
	return kit.Command{
		Use:   "version",
		Short: "Print version info",
		Run: func(ctx context.Context, args []string) error {
			_, err := fmt.Fprintf(os.Stdout, "x %s (commit %s, built %s, %s/%s)\n",
				Version, Commit, Date, runtime.GOOS, runtime.GOARCH)
			return err
		},
	}
}

// ---- helpers ----

func (a *App) printKVString(m map[string]string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out, err := a.out()
	if err != nil {
		return err
	}
	for _, k := range keys {
		if err := out.Emit(Row{Cols: []string{"key", "value"}, Vals: []string{k, m[k]},
			Value: map[string]any{"key": k, "value": m[k]}}); err != nil {
			return err
		}
	}
	return out.Flush()
}

func orNone(s string) string {
	if s == "" {
		return "(auto)"
	}
	return s
}

var cookieRe = regexp.MustCompile(`(auth_token|ct0)=([A-Za-z0-9%_\-]+)`)

func parseCookieHeader(s string) (authToken, ct0 string) {
	for _, m := range cookieRe.FindAllStringSubmatch(s, -1) {
		switch m[1] {
		case "auth_token":
			authToken = m[2]
		case "ct0":
			ct0 = m[2]
		}
	}
	return
}

func readStdin() string {
	fi, err := os.Stdin.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) != 0 {
		return "" // a terminal with no piped input
	}
	b, _ := io.ReadAll(bufio.NewReader(os.Stdin))
	return string(b)
}

func refURL(s string) string {
	if id, err := x.ParseTweetRef(s); err == nil {
		return x.TweetURL("i/web", id)
	}
	if ref, _, err := x.ParseUserRef(s, false); err == nil {
		return x.UserURL(ref)
	}
	return s
}

func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	return exec.Command(cmd, append(args, url)...).Start()
}

func bestMediaURL(m x.Media) string {
	if len(m.Variants) > 0 {
		best, rate := "", -1
		for _, v := range m.Variants {
			if v.Bitrate >= rate && v.URL != "" {
				best, rate = v.URL, v.Bitrate
			}
		}
		if best != "" {
			return best
		}
	}
	return m.URL
}

func extOf(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	if e := filepath.Ext(u); e != "" {
		return e
	}
	return ".bin"
}

func downloadFile(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", x.UserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, resp.Body)
	return err
}
