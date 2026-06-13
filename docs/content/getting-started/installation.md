---
title: "Installation"
description: "Install x from a release, with go install, or from source. Pure Go, no runtime dependencies."
weight: 20
---

## Prebuilt binaries

Every [release](https://github.com/tamnd/x-cli/releases) carries archives for
Linux, macOS, and Windows on amd64 and arm64, plus deb, rpm, and apk packages
for Linux. Download, unpack, put `x` on your `PATH`, done. The `checksums.txt`
on each release is signed with keyless [cosign](https://docs.sigstore.dev/) if
you want to verify before running.

## With Go

```bash
go install github.com/tamnd/x-cli/cmd/x@latest
```

That puts `x` in `$(go env GOPATH)/bin`, which is `~/go/bin` unless you moved
it. Make sure that directory is on your `PATH`.

## From source

```bash
git clone https://github.com/tamnd/x-cli
cd x-cli
make build        # produces ./bin/x
./bin/x version
```

## Pure Go

The binary is pure Go and builds with `CGO_ENABLED=0`, so it cross-compiles
cleanly and has no runtime dependencies. The local SQLite store uses a pure-Go
driver, so there is no C toolchain or shared library to install. There is no
config file to create, no database to provision, and no daemon to run.

## Requirements

- **Go 1.26 or later** to build. The released binary has no Go requirement.

That is the whole list.

## Shell completion

x can generate a completion script for your shell:

```bash
x completion bash > /etc/bash_completion.d/x
x completion zsh  > "${fpath[1]}/_x"
x completion fish > ~/.config/fish/completions/x.fish
```

Run `x completion --help` for the per-shell install instructions.

## Checking the install

```bash
x version
```

prints the version and exits. Then confirm it can reach X:

```bash
x tweet 20
```

should print the first tweet ever, "just setting up my twttr". If you see it,
you are ready for the [quick start](/getting-started/quick-start/).
