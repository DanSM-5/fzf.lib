package fzf

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/DanSM-5/fzf.lib/src/util"
)

func isDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func newLibContextTestTerminal(s *libState) *Terminal {
	return &Terminal{lib: s, reqBox: util.NewEventBox(), startChan: make(chan fitpad, 1)}
}

func hasRequest(t *Terminal, req util.EventType) bool {
	return t.reqBox.Peek(req)
}

func TestRunContextRequiresParse(t *testing.T) {
	opts, _ := ParseOptions(false, nil)
	if code, err := RunContext(context.Background(), opts); code != ExitError || err == nil {
		t.Errorf("code=%d err=%v", code, err)
	}
}

func TestLibContextLifecycle(t *testing.T) {
	s := &libState{}
	if ctx := s.context(false); ctx != context.Background() {
		t.Error("callbacks outside of Run should get a background context")
	}

	cleanup := s.start(&Options{})
	run, bg := s.context(false), s.context(true)
	if isDone(run) || isDone(bg) {
		t.Fatal("contexts should be active during Run")
	}

	// bg-cancel cancels the running background functions only, and the next
	// ones get a new context
	s.cancelBackground()
	if !isDone(bg) || isDone(run) {
		t.Errorf("bg-cancel: bg=%v run=%v", isDone(bg), isDone(run))
	}
	bg = s.context(true)
	if isDone(bg) {
		t.Error("new background context should be active")
	}

	cleanup()
	if !isDone(run) || !isDone(bg) {
		t.Error("contexts should be cancelled when Run returns")
	}
	if s.error() != nil {
		t.Errorf("Run without a context should not fail: %v", s.error())
	}
	(*libState)(nil).cancelBackground() // CLI options
}

func TestLibContextCancelAfterAttach(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &libState{ctx: ctx}
	cleanup := s.start(&Options{})
	defer cleanup()
	term := newLibContextTestTerminal(s)
	s.attach(term)

	run := s.context(false)
	cancel()
	select {
	case <-term.startChan:
	case <-time.After(time.Second):
		t.Fatal("terminal was not started to handle the interrupt")
	}
	if !hasRequest(term, reqQuit) {
		t.Error("quit was not requested")
	}
	if !errors.Is(s.error(), context.Canceled) {
		t.Errorf("error: %v", s.error())
	}
	if !isDone(run) {
		t.Error("callback context should be cancelled with the parent")
	}
}

func TestLibContextCancelBeforeAttach(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	s := &libState{ctx: ctx}
	cleanup := s.start(&Options{})
	defer cleanup()
	<-ctx.Done()
	// Let the watcher record the cancellation
	for start := time.Now(); ; time.Sleep(time.Millisecond) {
		s.mutex.Lock()
		cancelled := s.cancelled
		s.mutex.Unlock()
		if cancelled {
			break
		}
		if time.Since(start) > time.Second {
			t.Fatal("cancellation was not recorded")
		}
	}

	term := newLibContextTestTerminal(s)
	s.attach(term)
	if !hasRequest(term, reqQuit) || len(term.startChan) != 1 {
		t.Error("terminal attached after the cancellation should quit")
	}
	if !errors.Is(s.error(), context.DeadlineExceeded) {
		t.Errorf("error: %v", s.error())
	}
}

func TestLibContextFirstErrorWins(t *testing.T) {
	s := &libState{}
	s.setError(errors.New("first"))
	s.setError(errors.New("second"))
	if s.error().Error() != "first" {
		t.Errorf("error: %v", s.error())
	}
}

func TestCallLibCancelled(t *testing.T) {
	term := newLibContextTestTerminal(&libState{})
	ctx, cancel := context.WithCancel(context.Background())
	fn := func(ctx context.Context, s *State) string {
		cancel()
		<-ctx.Done()
		return "accept"
	}
	if out := term.callLib(ctx, fn, &State{}); out != "" {
		t.Errorf("output of a cancelled function should be discarded: %q", out)
	}

	fn = func(context.Context, *State) string { return "accept" }
	if out := term.callLib(context.Background(), fn, &State{}); out != "accept" {
		t.Errorf("output: %q", out)
	}
}

func TestCallLibPanic(t *testing.T) {
	term := newLibContextTestTerminal(&libState{})
	fn := func(context.Context, *State) string { panic("oops") }
	if out := term.callLib(context.Background(), fn, &State{Key: "ctrl-p"}); out != "" {
		t.Errorf("output: %q", out)
	}
	if !hasRequest(term, reqFatal) {
		t.Error("a panic should make fzf quit")
	}
	if err := term.lib.error(); err == nil || err.Error() != "panic in Go binding for ctrl-p: oops" {
		t.Errorf("error: %v", err)
	}
}

func TestPreviewContextFollowsRun(t *testing.T) {
	cancelled := make(chan bool, 1)
	s := &libState{preview: func(ctx context.Context, _ *State, _ io.Writer) error {
		<-ctx.Done()
		cancelled <- true
		return ctx.Err()
	}}
	cleanup := s.start(&Options{})
	term := &Terminal{lib: s}
	item := newItem("foo")
	p := term.newPreviewProcess(libPreviewPrefix, "", nil, [3][]*Item{{item}, {item}, nil})
	out, _ := p.StdoutPipe()
	p.Start()

	cleanup() // Run returns
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("preview function should be cancelled when Run returns")
	}
	io.ReadAll(out)
	p.Wait()
}

func TestProxyInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan string, 2)
	input <- "one"
	input <- "two"
	proxy := proxyInput(ctx, input)
	if <-proxy != "one" || <-proxy != "two" {
		t.Fatal("items should be forwarded in order")
	}

	// The consumer never closes input, but the proxy ends with ctx
	cancel()
	select {
	case _, more := <-proxy:
		if more {
			t.Error("no item should be forwarded after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("proxy was not closed")
	}

	input = make(chan string)
	proxy = proxyInput(context.Background(), input)
	close(input)
	if _, more := <-proxy; more {
		t.Error("proxy should be closed with the input")
	}
}

func TestRunContextRestoresInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan string)
	opts := &Options{Input: input}
	s := &libState{ctx: ctx}
	cleanup := s.start(opts)
	if opts.Input == input {
		t.Error("input should be proxied during RunContext")
	}
	cleanup()
	if opts.Input != input {
		t.Error("input should be restored when Run returns")
	}

	// Run without a context reads the input directly
	s = &libState{}
	cleanup = s.start(opts)
	if opts.Input != input {
		t.Error("input should not be proxied without a context")
	}
	cleanup()
}
