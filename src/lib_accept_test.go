package fzf

import (
	"reflect"
	"testing"
)

func runFilter(t *testing.T, opts *Options, items ...string) (int, []string) {
	t.Helper()
	input := make(chan string, len(items))
	for _, item := range items {
		input <- item
	}
	close(input)
	opts.Input = input
	printed := []string{}
	opts.Printer = func(s string) { printed = append(printed, s) }
	code, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	return code, printed
}

func TestAcceptedFilter(t *testing.T) {
	for _, args := range [][]string{
		{"--filter=b", "--no-sort"},
		{"--filter=b", "--no-sort", "--sync"}, // the non-streaming filter
	} {
		opts, err := Parse(LibOptions{Args: append(args, "--accept-nth=2")})
		if err != nil {
			t.Fatal(err)
		}
		code, printed := runFilter(t, opts, "a 1", "b 2", "c 3", "bb 4")
		if code != ExitOk || !reflect.DeepEqual(printed, []string{"2", "4"}) {
			t.Errorf("%v: code %d, printed %v", args, code, printed)
		}
		// The whole items, unaffected by --accept-nth
		want := []Entry{{1, "b 2"}, {3, "bb 4"}}
		if got := opts.Accepted(); !reflect.DeepEqual(got, want) {
			t.Errorf("%v: accepted %v, want %v", args, got, want)
		}

		// Reset by the next Run
		if code, _ := runFilter(t, opts, "x", "y"); code != ExitNoMatch || opts.Accepted() != nil {
			t.Errorf("%v: code %d, accepted %v after a run without a match", args, code, opts.Accepted())
		}
	}
}

func TestAcceptedRequiresParse(t *testing.T) {
	opts, err := ParseOptions(false, []string{"--filter=a"})
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := runFilter(t, opts, "a"); code != ExitOk || opts.Accepted() != nil {
		t.Errorf("code %d, accepted %v", code, opts.Accepted())
	}
}

func TestCheckTerminal(t *testing.T) {
	// needWinpty is only true on Windows in mintty
	opts := defaultOptions()
	if err := (&libState{}).checkTerminal(opts); err != nil && !needWinpty(opts) {
		t.Errorf("unexpected error: %v", err)
	}
	var s *libState
	if err := s.checkTerminal(opts); err != nil {
		t.Errorf("ParseOptions should keep the CLI behavior: %v", err)
	}
}
