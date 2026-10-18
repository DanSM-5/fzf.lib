// Example of using fzf.lib from a Go program.
//
//	go run ./examples/library
//
// enter      accept and print the selection
// ctrl-f     Go binding (BindSync) changing the prompt
// ctrl-b     Go binding (BindBackground) updating the header after a delay
// ctrl-e     Go binding (BindForeground) using the terminal directly
// ctrl-x     'become' returned to the caller instead of replacing the process
package main

import (
	"bufio"
	"context"
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

	opts.BindFunc("ctrl-f", fzf.BindSync, func(s *fzf.State) string {
		if s.Current == nil {
			return ""
		}
		return fmt.Sprintf("change-prompt(%s> )", fruits[s.Current.Index].color)
	})

	opts.BindFunc("ctrl-b", fzf.BindBackground, func(s *fzf.State) string {
		time.Sleep(500 * time.Millisecond)
		return fmt.Sprintf("change-header(%d of %d match)", s.MatchCount, s.TotalCount)
	})

	opts.BindFunc("ctrl-e", fzf.BindForeground, func(s *fzf.State) string {
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

	var selected []string
	output := make(chan string)
	opts.Output = output
	done := make(chan bool)
	go func() {
		for line := range output {
			selected = append(selected, line)
		}
		done <- true
	}()

	code, err := fzf.Run(opts)
	close(output)
	<-done

	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, "error:", err)
	case code == fzf.ExitBecome:
		fmt.Println("become:", strings.TrimSpace(opts.Become().Command))
	default:
		fmt.Printf("exit %d: %s\n", code, strings.Join(selected, ", "))
	}
	os.Exit(code)
}
