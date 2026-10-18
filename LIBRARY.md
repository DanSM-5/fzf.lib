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
opts.Output = output

// Preview rendered in Go (ANSI colors supported, streamed as it is written)
opts.SetPreviewFunc(func(ctx context.Context, s *fzf.State, w io.Writer) error {
    fmt.Fprintln(w, describe(s.Current))
    return nil
}, fzf.PreviewOnFocus)

// Go bindings return follow-up actions in the --bind syntax
opts.BindFunc("ctrl-f", fzf.BindSync, func(s *fzf.State) string {
    return "change-prompt(filtered> )"
})

// Regular string bindings are still available
opts.Bind("ctrl-r:reload(ls)+first")

code, err := fzf.Run(opts)
```

| Bind mode        | Runs                                         | Like            |
| ---------------- | -------------------------------------------- | --------------- |
| `BindSync`       | on the event loop; the UI waits for it       | `transform`     |
| `BindBackground` | in a goroutine; actions applied on return    | `bg-transform`  |
| `BindForeground` | with the UI suspended; may use the terminal  | `execute`       |

Differences from the CLI when using `Parse`:

- `--tmux` is ignored, as the host program cannot be re-run in a popup.
- `become(...)` makes `Run` return `ExitBecome`; the command is in
  `opts.Become()`.
- A panic in a Go binding ends fzf and `Run` returns it as the error. A
  panic or error in a preview function is shown in the preview window.
- `--scheme` is decided when `Run` is called: `path` only when the built-in
  walker provides the list.
- One `Run` at a time per process.

Merging from upstream
---------------------

Library code lives in `src/lib*.go`. Upstream files only contain small hooks,
each marked with a `fzf.lib` comment (`grep -rn 'fzf.lib' src`).

```sh
git remote add upstream https://github.com/junegunn/fzf.git  # once
git fetch upstream && git merge upstream/master
scripts/rewrite-module-path.sh   # new upstream files import github.com/junegunn/fzf
go build ./... && go test ./...
```

Merge conflicts in import blocks come from the module path rename: take
upstream's side and re-run the script.
