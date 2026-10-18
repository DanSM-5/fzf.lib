// Example of using fzf.lib from a Go program.
//
//	go run ./examples/library
//
// enter      accept and print the selection
// ctrl-f     Go binding (BindSync) changing the prompt
// ctrl-b     Go binding (BindBackground) updating the header after a delay
// ctrl-e     Go binding (BindForeground) using the terminal directly
// ctrl-x     'become' returned to the caller instead of replacing the process
// ctrl-z     cancel a running ctrl-b binding ('bg-cancel')
//
// The picker quits on its own after one minute (RunContext).
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	fzf "github.com/DanSM-5/fzf.lib/src"
)

type fruit struct {
	name  string
	color string
}

var fruits = []fruit{
	{"apple", "red"},
	{"banana", "yellow"},
	{"cherry", "red"},
	{"grape", "purple"},
	{"lemon", "yellow"},
	{"lime", "green"},
}

func main() {
	opts, err := fzf.Parse(fzf.LibOptions{
		UseDefaultOpts: true, // appearance from $FZF_DEFAULT_OPTS
		Args:           []string{"--multi", "--layout=reverse", "--header=fruits", "--bind=ctrl-x:become(echo {})"},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(fzf.ExitError)
	}

	// The preview is rendered by Go. Index refers to the position in the input.
	opts.SetPreviewFunc(func(ctx context.Context, s *fzf.State, w io.Writer) error {
		if s.Current == nil {
			fmt.Fprintf(w, "no match for %q\n", s.Query)
			return nil
		}
		f := fruits[s.Current.Index]
		fmt.Fprintf(w, "name:  %s\ncolor: %s\nquery: %s\nselected: %d\n", f.name, f.color, s.Query, len(s.Selected))
		return nil
	}, fzf.PreviewOnQuery)

	opts.BindFunc("ctrl-f", fzf.BindSync, func(ctx context.Context, s *fzf.State) string {
		if s.Current == nil {
			return ""
		}
		return fmt.Sprintf("change-prompt(%s> )", fruits[s.Current.Index].color)
	})

	// Long-running functions should stop when ctx is cancelled: on 'bg-cancel',
	// when fzf quits, or when the context given to RunContext is done
	opts.BindFunc("ctrl-b", fzf.BindBackground, func(ctx context.Context, s *fzf.State) string {
		select {
		case <-time.After(2 * time.Second):
			return fmt.Sprintf("change-header(%d of %d match)", s.MatchCount, s.TotalCount)
		case <-ctx.Done():
			return ""
		}
	})
	opts.Bind("ctrl-z:bg-cancel+change-header(cancelled)")

	opts.BindFunc("ctrl-e", fzf.BindForeground, func(ctx context.Context, s *fzf.State) string {
		fmt.Print("foreground: press enter to return ")
		bufio.NewReader(os.Stdin).ReadString('\n')
		return "change-header(back from foreground)"
	})

	input := make(chan string)
	opts.Input = input
	go func() {
		for _, f := range fruits {
			input <- f.name
		}
		close(input)
	}()

	// Accepted is used below instead of reading the printed lines
	opts.Printer = func(string) {}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	code, err := fzf.RunContext(ctx, opts)

	var selected []string
	for _, e := range opts.Accepted() {
		selected = append(selected, fruits[e.Index].name+" ("+fruits[e.Index].color+")")
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		fmt.Println("timed out")
	case err != nil:
		fmt.Fprintln(os.Stderr, "error:", err)
	case code == fzf.ExitBecome:
		fmt.Println("become:", strings.TrimSpace(opts.Become().Command))
	default:
		fmt.Printf("exit %d: %s\n", code, strings.Join(selected, ", "))
	}
	os.Exit(code)
}
