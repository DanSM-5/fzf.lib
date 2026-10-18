package fzf

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DanSM-5/fzf.lib/src/tui"
)

func TestParseDefaultOptsToggles(t *testing.T) {
	file := filepath.Join(t.TempDir(), "fzfrc")
	if err := os.WriteFile(file, []byte("--prompt=file:"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FZF_DEFAULT_OPTS_FILE", file)
	t.Setenv("FZF_DEFAULT_OPTS", "--ghost=env")

	for _, tc := range []struct {
		useFile, useEnv bool
		prompt, ghost   string
	}{
		{false, false, "> ", ""},
		{true, false, "file:", ""},
		{false, true, "> ", "env"},
		{true, true, "file:", "env"},
	} {
		opts, err := Parse(LibOptions{UseDefaultOptsFile: tc.useFile, UseDefaultOpts: tc.useEnv})
		if err != nil {
			t.Fatal(err)
		}
		if opts.Prompt != tc.prompt || opts.Ghost != tc.ghost {
			t.Errorf("file=%v env=%v: prompt=%q ghost=%q", tc.useFile, tc.useEnv, opts.Prompt, opts.Ghost)
		}
	}
}

func TestParseIgnoresBrokenDisabledDefaults(t *testing.T) {
	t.Setenv("FZF_DEFAULT_OPTS", "--no-such-option")
	t.Setenv("FZF_DEFAULT_OPTS_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := Parse(LibOptions{}); err != nil {
		t.Errorf("disabled defaults should not be read: %v", err)
	}
	if _, err := Parse(LibOptions{UseDefaultOpts: true}); err == nil {
		t.Error("expected an error from $FZF_DEFAULT_OPTS")
	}
	if _, err := Parse(LibOptions{UseDefaultOptsFile: true}); err == nil {
		t.Error("expected an error from $FZF_DEFAULT_OPTS_FILE")
	}
}

func TestParseArgsAfterDefaults(t *testing.T) {
	t.Setenv("FZF_DEFAULT_OPTS", "--prompt=env>")
	opts, err := Parse(LibOptions{UseDefaultOpts: true, Args: []string{"--prompt=args>"}})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Prompt != "args>" {
		t.Errorf("prompt: %q", opts.Prompt)
	}
}

func TestParseLibraryMode(t *testing.T) {
	t.Setenv("FZF_DEFAULT_OPTS", "--tmux")
	opts, err := Parse(LibOptions{UseDefaultOpts: true})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Tmux != nil {
		t.Error("--tmux should be ignored in library mode")
	}
	if opts.Scheme != "default" || !opts.lib.autoScheme {
		t.Errorf("scheme should be decided by Run: %q %v", opts.Scheme, opts.lib.autoScheme)
	}

	// Given input is never treated as file paths
	opts.Input = make(chan string)
	opts.lib.prepare(opts)
	if opts.Scheme != "default" {
		t.Errorf("scheme: %q", opts.Scheme)
	}

	opts, _ = Parse(LibOptions{Args: []string{"--scheme=history"}})
	if opts.lib.autoScheme {
		t.Error("explicit --scheme should be kept")
	}
}

func TestDefaultCommandToggle(t *testing.T) {
	t.Setenv("FZF_DEFAULT_COMMAND", "echo hello")
	defer func() { useDefaultCommand = true }()

	opts, _ := Parse(LibOptions{})
	opts.lib.prepare(opts)
	if cmd := defaultCommand(); cmd != "" {
		t.Errorf("default command should be disabled: %q", cmd)
	}
	opts, _ = Parse(LibOptions{UseDefaultCommand: true})
	opts.lib.prepare(opts)
	if cmd := defaultCommand(); cmd != "echo hello" {
		t.Errorf("default command: %q", cmd)
	}
	// Options from ParseOptions (CLI) always use it
	(*libState)(nil).prepare(nil)
	if cmd := defaultCommand(); cmd != "echo hello" {
		t.Errorf("default command: %q", cmd)
	}
}

func TestBindFunc(t *testing.T) {
	t.Setenv("FZF_DEFAULT_OPTS", "--bind=ctrl-f:first")
	opts, _ := Parse(LibOptions{UseDefaultOpts: true})
	called := false
	if err := opts.BindFunc("ctrl-f,f5", BindBackground, func(*State) string { called = true; return "" }); err != nil {
		t.Fatal(err)
	}
	for _, evt := range []tui.Event{tui.CtrlF.AsEvent(), tui.F5.AsEvent()} {
		acts := opts.Keymap[evt]
		if len(acts) != 1 || acts[0].t != actLibFuncBg || acts[0].a != "0" {
			t.Errorf("%v: %+v", evt, acts)
		}
	}
	opts.lib.funcs[0](nil)
	if !called {
		t.Error("function not registered")
	}
	if name := actLibFuncBg.Name(); name != "bg-go" {
		t.Errorf("name: %q", name)
	}
	if !processExecution(actLibFunc) {
		t.Error("Go bindings should not be recorded as the last action")
	}

	if err := opts.BindFunc("no-such-key", BindSync, func(*State) string { return "" }); err == nil {
		t.Error("expected an error for an invalid key")
	}
	if err := opts.BindFunc("ctrl-g", BindMode(42), func(*State) string { return "" }); err == nil {
		t.Error("expected an error for an invalid mode")
	}
	cli, _ := ParseOptions(false, nil)
	if err := cli.BindFunc("ctrl-g", BindSync, func(*State) string { return "" }); err == nil {
		t.Error("expected an error for Options not created by Parse")
	}
}

func TestLibBind(t *testing.T) {
	opts, _ := Parse(LibOptions{})
	if err := opts.Bind("ctrl-r:reload(ls)+first"); err != nil {
		t.Fatal(err)
	}
	acts := opts.Keymap[tui.CtrlR.AsEvent()]
	if len(acts) != 2 || acts[0].t != actReload || acts[0].a != "ls" || acts[1].t != actFirst {
		t.Errorf("%+v", acts)
	}
}

func TestSetPreviewFunc(t *testing.T) {
	opts, _ := Parse(LibOptions{Args: []string{"--preview=cat {}"}})
	fn := func(context.Context, *State, io.Writer) error { return nil }
	if err := opts.SetPreviewFunc(fn, PreviewOnFocus); err != nil {
		t.Fatal(err)
	}
	if _, plus, _, force := hasPreviewFlags(opts.Preview.command); !plus || force {
		t.Errorf("PreviewOnFocus: %q", opts.Preview.command)
	}
	if err := opts.SetPreviewFunc(fn, PreviewOnQuery); err != nil {
		t.Fatal(err)
	}
	if _, _, _, force := hasPreviewFlags(opts.Preview.command); !force {
		t.Errorf("PreviewOnQuery: %q", opts.Preview.command)
	}
}

func newLibTestTerminal(preview PreviewFunc) *Terminal {
	return &Terminal{lib: &libState{preview: preview}}
}

func readPreview(t *testing.T, p *previewProcess) string {
	t.Helper()
	out, _ := p.StdoutPipe()
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	bytes, _ := io.ReadAll(out)
	p.Wait()
	return string(bytes)
}

func TestPreviewProcessFunc(t *testing.T) {
	var got *State
	term := newLibTestTerminal(func(ctx context.Context, s *State, w io.Writer) error {
		got = s
		io.WriteString(w, "line 1\nline 2\n")
		return nil
	})
	item := newItem("foo")
	env := []string{"HOME=/x", "FZF_QUERY=fo", "FZF_SELECT_COUNT=0", "FZF_PREVIEW_LINES=10", "FZF_PREVIEW_COLUMNS=40"}
	p := term.newPreviewProcess(libPreviewPrefix+" {+}", "ignored", env, [3][]*Item{{item}, {item}, nil})
	if out := readPreview(t, p); out != "line 1\nline 2\n" {
		t.Errorf("output: %q", out)
	}
	if got.Query != "fo" || got.Current == nil || got.Current.Text != "foo" ||
		len(got.Selected) != 0 || got.PreviewLines != 10 || got.PreviewColumns != 40 {
		t.Errorf("state: %+v", got)
	}
	if _, prs := got.Env["HOME"]; prs {
		t.Error("only FZF_* variables should be in Env")
	}

	// No match
	p = term.newPreviewProcess(libPreviewPrefix+" {+} {q}", "", nil, [3][]*Item{{&minItem}, {&minItem}, nil})
	readPreview(t, p)
	if got.Current != nil {
		t.Errorf("current: %+v", got.Current)
	}
}

func TestPreviewProcessErrors(t *testing.T) {
	item := newItem("foo")
	items := [3][]*Item{{item}, {item}, nil}

	term := newLibTestTerminal(func(context.Context, *State, io.Writer) error { return errors.New("boom") })
	if out := readPreview(t, term.newPreviewProcess(libPreviewPrefix, "", nil, items)); out != "boom\n" {
		t.Errorf("error output: %q", out)
	}

	term = newLibTestTerminal(func(context.Context, *State, io.Writer) error { panic("oops") })
	if out := readPreview(t, term.newPreviewProcess(libPreviewPrefix, "", nil, items)); !strings.Contains(out, "panic in preview function: oops") {
		t.Errorf("panic output: %q", out)
	}
}

func TestPreviewProcessKill(t *testing.T) {
	item := newItem("foo")
	items := [3][]*Item{{item}, {item}, nil}

	// A function that ignores ctx and blocks forever must not stall the preview loop
	term := newLibTestTerminal(func(ctx context.Context, s *State, w io.Writer) error {
		io.WriteString(w, "partial\n")
		select {}
	})
	p := term.newPreviewProcess(libPreviewPrefix, "", nil, items)
	out, _ := p.StdoutPipe()
	p.Start()
	buf := make([]byte, 8)
	if n, _ := io.ReadFull(out, buf); string(buf[:n]) != "partial\n" {
		t.Errorf("partial output: %q", buf[:n])
	}
	done := make(chan bool)
	go func() {
		io.ReadAll(out)
		p.Wait()
		done <- true
	}()
	p.Kill()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("preview was not cancelled")
	}

	// A well-behaved function sees the cancellation
	cancelled := make(chan bool, 1)
	term = newLibTestTerminal(func(ctx context.Context, s *State, w io.Writer) error {
		<-ctx.Done()
		cancelled <- true
		return ctx.Err()
	})
	p = term.newPreviewProcess(libPreviewPrefix, "", nil, items)
	out, _ = p.StdoutPipe()
	p.Start()
	p.Kill()
	io.ReadAll(out)
	p.Wait()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("ctx was not cancelled")
	}
}
