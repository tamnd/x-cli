package x

import (
	"context"
	"errors"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes X as a kit Domain: a driver that a multi-domain host (ant)
// enables with a single blank import,
//
//	import _ "github.com/tamnd/x-cli/x"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// x:// URIs by routing to the operations Register installs. The standalone x
// binary does not use any of this, so the CLI is unchanged.
func init() { kit.Register(Domain{}) }

// Domain is the X driver. It carries no state; the per-run Engine is built by
// the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity a host reuses for help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme:  "x",
		Aliases: []string{"twitter"},
		Hosts:   []string{"x.com", "twitter.com"},
		Identity: kit.Identity{
			Binary: "x",
			Short:  "Read public X (Twitter) data",
			Site:   "x.com",
			Repo:   "https://github.com/tamnd/x-cli",
		},
	}
}

// Register installs the Engine factory and every X operation onto app. The
// operations themselves are in ops.go, shared with the standalone binary, which
// registers the same set with NoCLI so its hand-written commands keep the verbs.
// Here they take the command line too, because in a host the operations are the
// command line.
func (Domain) Register(app *kit.App) {
	app.SetClient(newEngine)
	RegisterOps(app, OpOptions{})
}

// newEngine builds the X engine from the host-resolved config, reusing the same
// data dir and environment the standalone binary uses so a lent session (the
// user's own cookies) and the page cache are shared.
func newEngine(_ context.Context, cfg kit.Config) (any, error) {
	o := NoOverrides()
	o.DataDir = cfg.DataDir
	o.NoCache = cfg.NoCache
	return NewEngine(Resolve(o)), nil
}

// --- Resolver: the URI-native string functions, reused from ref.go ---

// Classify turns any accepted input into the canonical (kind, id), so `ant
// resolve` and `ant url` need no network. It is the same pure layer `x classify`
// uses, because a host and the standalone binary disagreeing about what a link
// names would put the same tweet in the graph twice.
func (Domain) Classify(input string) (uriType, id string, err error) {
	kind, id, cerr := Classify(input)
	if cerr != nil {
		return "", "", errs.Usage("unrecognized X reference: %q", input)
	}
	return kind, id, nil
}

// Locate is the inverse: the live page URL for a (kind, id).
func (Domain) Locate(uriType, id string) (string, error) {
	u, err := Locate(uriType, id)
	if err != nil {
		return "", errs.Usage("%s", err.Error())
	}
	return u, nil
}

// mapErr converts a library error into the kit error kind that carries the right
// exit code, so a host renders the same need-auth/rate-limited outcomes the
// standalone binary does.
//
// It covers the whole taxonomy rather than the two that a host happened to need
// first, because these handlers now answer over HTTP and to an agent as well.
// Need-auth and unsupported are the pair worth getting right: one means find a
// credential and this works, the other means no credential exists that would
// help, and a caller that cannot tell them apart will go looking for a cookie
// that was never the problem.
func mapErr(err error) error {
	var na *NeedAuthError
	var rl *RateLimitedError
	var nf *NotFoundError
	var un *UnsupportedError
	var ne *NetworkError
	// A walk that stopped halfway is classified by whatever stopped it, and says
	// how far it got in the message.
	said := func(inner error) string {
		var pe *PartialError
		if errors.As(err, &pe) {
			return pe.Error()
		}
		return inner.Error()
	}
	switch {
	case err == nil:
		return nil
	case errors.As(err, &na):
		return errs.NeedAuth("%s", said(na))
	case errors.As(err, &rl):
		return errs.RateLimited("%s", said(rl))
	case errors.As(err, &nf):
		return errs.NotFound("%s", said(nf))
	case errors.As(err, &un):
		return errs.Unsupported("%s", said(un))
	case errors.As(err, &ne):
		return errs.Network("%s", said(ne))
	}
	if n := AsNetwork(err); n != nil {
		return errs.Network("%s", n.Error())
	}
	return err
}
