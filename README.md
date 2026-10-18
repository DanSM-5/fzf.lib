fzf.lib
=======

**fzf.lib** is a fork of [fzf](https://github.com/junegunn/fzf), the
command-line fuzzy finder by Junegunn Choi, packaged as a Go library for Go
programs.

Motivation
----------

fzf already exposes Go functions (`ParseOptions`, `Run`), but they are built
for the command line: everything is a string. The preview window, key
bindings, `reload`, `transform` and so on all run shell commands, and some
features assume fzf is its own process. `--tmux` re-executes `os.Args`, and
`become` replaces the running process.

fzf.lib keeps fzf's interface and option parser, and adds what a host program
needs:

- **Go callbacks:** key bindings and the preview window can call Go functions
  instead of shell commands.
- **Control over user configuration:** `$FZF_DEFAULT_OPTS`,
  `$FZF_DEFAULT_OPTS_FILE` and `$FZF_DEFAULT_COMMAND` can each be turned on or
  off, so the host decides how much the user can customize.
- **Cancellation:** `RunContext` closes the picker when a `context.Context` is
  done, for example on a timeout. Go callbacks receive a context too.
- **Safe for a host process:** fzf never re-executes or replaces the program
  that embeds it.

The fork tracks upstream closely so that fzf's improvements keep flowing in.
The library code is kept in separate files, and changes to upstream files are
small, marked hooks. It is not meant to build the `fzf` command; use
[upstream fzf](https://github.com/junegunn/fzf) for that.

Installation
------------

```sh
go get github.com/DanSM-5/fzf.lib@master
```

Usage
-----

```go
package main

import (
	"context"
	"fmt"
	"io"
	"time"

	fzf "github.com/DanSM-5/fzf.lib/src"
)

func main() {
	opts, err := fzf.Parse(fzf.LibOptions{
		UseDefaultOpts: true, // respect the user's $FZF_DEFAULT_OPTS
		Args:           []string{"--multi", "--layout=reverse"},
	})
	if err != nil {
		panic(err)
	}

	// Preview rendered by Go
	opts.SetPreviewFunc(func(ctx context.Context, s *fzf.State, w io.Writer) error {
		fmt.Fprintf(w, "item #%d: %s\n", s.Current.Index, s.Current.Text)
		return nil
	}, fzf.PreviewOnFocus)

	// Go key binding; returns more fzf actions to run
	opts.BindFunc("ctrl-f", fzf.BindSync, func(ctx context.Context, s *fzf.State) string {
		return "change-prompt(Go> )"
	})

	input := make(chan string)
	output := make(chan string, 100)
	opts.Input, opts.Output = input, output
	go func() {
		defer close(input)
		for _, item := range []string{"apple", "banana", "cherry"} {
			input <- item
		}
	}()

	// Close the picker after a minute
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	code, err := fzf.RunContext(ctx, opts)
	close(output)
	for selected := range output {
		fmt.Println(selected)
	}
	fmt.Println("exit code:", code, "error:", err)
}
```

See [LIBRARY.md](LIBRARY.md) for the full API, including bind modes,
cancellation, `become` and differences from the CLI, and for how to merge from
upstream. [examples/library](examples/library/main.go) is a complete runnable
program:

```sh
go run ./examples/library
```

The options passed in `Args` are fzf's command-line options; see the
[fzf documentation](https://github.com/junegunn/fzf#usage) and the man page in
[man/man1/fzf.1](man/man1/fzf.1).

[License](LICENSE)
------------------

The MIT License (MIT)

Copyright (c) 2013-2026 Junegunn Choi

fzf.lib is based on fzf. If you find it useful, consider
[sponsoring fzf's author](https://github.com/sponsors/junegunn).
