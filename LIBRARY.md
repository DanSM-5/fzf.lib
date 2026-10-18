fzf.lib
=======

A fork of [fzf](https://github.com/junegunn/fzf) meant to be used as a Go
library. Bindings and the preview window accept Go functions, and fzf never
replaces or re-executes the host process.

```sh
go get github.com/DanSM-5/fzf.lib
```

A complete program is in [examples/library](examples/library/main.go).

Usage
-----

```go
import fzf "github.com/DanSM-5/fzf.lib/src"

opts, err := fzf.Parse(fzf.LibOptions{
    UseDefaultOpts:     true,  // apply $FZF_DEFAULT_OPTS
    UseDefaultOptsFile: false, // ignore $FZF_DEFAULT_OPTS_FILE
    UseDefaultCommand:  false, // ignore $FZF_DEFAULT_COMMAND
    Args:               []string{"--multi", "--layout=reverse"},
})

// Items come from a channel; State.Current.Index is the position in it
opts.Input = input
// The printed lines go to opts.Output (a channel) or opts.Printer (a function);
// opts.Accepted() returns the same items with their index after Run
opts.Printer = func(string) {}

// Preview rendered in Go (ANSI colors supported, streamed as it is written)
opts.SetPreviewFunc(func(ctx context.Context, s *fzf.State, w io.Writer) error {
    fmt.Fprintln(w, describe(s.Current))
    return nil
}, fzf.PreviewOnFocus)

// Go bindings return follow-up actions in the --bind syntax
opts.BindFunc("ctrl-f", fzf.BindSync, func(ctx context.Context, s *fzf.State) string {
    return "change-prompt(filtered> )"
})

// Regular string bindings are still available
opts.Bind("ctrl-r:reload(ls)+first")

// Quit the picker after a minute; or use fzf.Run(opts) without a context
ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
defer cancel()
code, err := fzf.RunContext(ctx, opts)
if errors.Is(err, context.DeadlineExceeded) { /* code == fzf.ExitInterrupt */ }

// The selected items, or the current one without --multi
for _, e := range opts.Accepted() {
    use(values[e.Index])
}
```

| Bind mode        | Runs                                         | Like            |
| ---------------- | -------------------------------------------- | --------------- |
| `BindSync`       | on the event loop; the UI waits for it       | `transform`     |
| `BindBackground` | in a goroutine; actions applied on return    | `bg-transform`  |
| `BindForeground` | with the UI suspended; may use the terminal  | `execute`       |

Cancellation
------------

The `ctx` given to Go bindings and the preview function is cancelled when:

- fzf quits, or the context given to `RunContext` is done (all functions)
- `bg-cancel` is triggered (`BindBackground` functions)
- the preview is no longer needed, e.g. the cursor moved (preview function)

The result of a cancelled binding is discarded. Functions that ignore `ctx`
cannot be stopped, but fzf does not wait for them, except for `BindSync` and
`BindForeground`, which hold the interface until they return.

When the context of `RunContext` is done, it returns `ExitInterrupt` and
`ctx.Err()`, and `Options.Input` is no longer read, so the producer should also
stop on `ctx`. `--filter` is not interrupted.

Differences from the CLI when using `Parse`:

- fzf never re-runs the host program: `--tmux` and `--zellij` are ignored,
  and mintty on Windows is reported as an error (see below).
- `become(...)` makes `Run` return `ExitBecome`; the command is in
  `opts.Become()`.
- A panic in a Go binding ends fzf and `Run` returns it as the error. A
  panic or error in a preview function is shown in the preview window.
- `--scheme` is decided when `Run` is called: `path` only when the built-in
  walker provides the list.
- One `Run` at a time per process. The same `Options` can be passed to `Run`
  again, with a new `Input` channel; `Accepted()` and `Become()` then describe
  the last `Run`.

### mintty (Git Bash, MSYS2, Cygwin)

mintty, the default terminal of Git for Windows and MSYS2, is not a Windows
console, so fzf cannot use it directly. The CLI works around it by re-running
itself, through `winpty` or with `MSYS=enable_pcon` in a child process, which a
library cannot do to its host program. In mintty, `Run` returns an error unless:

- `MSYS=enable_pcon` is set before the program starts (mintty 3.4.5 or later),
  e.g. `export MSYS=enable_pcon` in `~/.bashrc`. It makes the MSYS runtime
  give programs a Windows pseudo console (ConPTY).
- `--no-winpty` is passed, on older mintty versions.

Windows Terminal, conhost and other console hosts need nothing, including
Git Bash or MSYS2 shells running inside them.

Merging from upstream
---------------------

Library code lives in `src/lib*.go`. Upstream files only contain small hooks,
each marked with a `fzf.lib` comment (`grep -rn 'fzf.lib' src`).

### Setup after cloning

Git configuration is not part of the repository, so run this once in every
new clone before merging:

```sh
git remote add upstream https://github.com/junegunn/fzf.git
git config remote.upstream.tagOpt --no-tags
git config merge.ours.driver true
```

`tagOpt --no-tags` keeps upstream's release tags out of the clone, see
[Versioning](#versioning). Set it before the first `git fetch upstream`. A clone
that already fetched them can drop them with:

```sh
git tag -l 'v*' | grep -v -- '-lib\.' | xargs -r git tag -d
```

`.gitattributes` marks files this fork rewrote, such as `README.md`, with
`merge=ours`, so that a merge keeps our version even when upstream changed
them. The `ours` driver is not built into git; the `git config` line defines
it as a no-op command (`true`) that leaves our file as it is. Without it, git
silently merges these files normally, which leads to conflicts.

To keep another file on merges, add it to `.gitattributes`.

### Merging

```sh
git fetch upstream && git merge upstream/master
scripts/rewrite-module-path.sh   # new upstream files import github.com/junegunn/fzf
go build ./... && go test ./...
```

Merge conflicts in import blocks come from the module path rename: take
upstream's side and re-run the script.

Versioning
----------

Releases are tagged `v<fzf version>-lib.<n>`:

- `<fzf version>` is the upstream release the fork is based on, so it tells
  which fzf features are supported, e.g. `v0.74.5-lib.1`.
- `<n>` is the library release for that fzf version, starting at 1 and
  restarting at 1 after each merge of a new upstream release:
  `v0.74.5-lib.1`, `v0.74.5-lib.2`, then `v0.75.0-lib.1`.

```sh
git tag -a v0.74.5-lib.1 -m 'fzf.lib v0.74.5-lib.1'
git push origin v0.74.5-lib.1   # push the one tag, not --tags
```

Programs depend on it with:

```sh
go get github.com/DanSM-5/fzf.lib@v0.74.5-lib.1   # or @latest
```

Why this format:

- Go only recognizes `vX.Y.Z` tags as versions of a module at the repository
  root. Any other tag, such as `lib-v0.74.5`, can still be fetched by name, but
  `go.mod` then records a pseudo-version (`v0.74.6-0.<date>-<hash>`) instead.
- `-lib.<n>` is a semver pre-release suffix. Numeric parts compare as
  numbers, so `-lib.10` is newer than `-lib.2`.
- Pre-releases sort before the release with the same number (`v0.74.5-lib.1` <
  `v0.74.5`). The fork must therefore not publish upstream's `v*` tags:
  `@latest` would pick upstream's `v0.74.5`, whose `go.mod` declares
  `github.com/junegunn/fzf`, and fail. That is why `tagOpt --no-tags` is part of
  the setup, and why only single tags are pushed.
