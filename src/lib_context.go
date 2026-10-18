package fzf

// fzf.lib: cancellation of the picker and of Go callbacks

import (
	"context"
	"errors"
)

// RunContext is like Run, but fzf quits when ctx is done, e.g. on a timeout.
// It then returns ExitInterrupt and ctx.Err().
//
// ctx is also the parent of the contexts given to Go bindings and the preview
// function, so they are cancelled with it, and Options.Input is no longer read
// once it is done.
//
// Note that --filter does not stop when ctx is done, and that with --sync,
// fzf can quit only after a command providing the list finishes.
func RunContext(ctx context.Context, opts *Options) (int, error) {
	if opts.lib == nil {
		return ExitError, errors.New("RunContext requires Options created by Parse")
	}
	if ctx == nil {
		return ExitError, errors.New("RunContext requires a context")
	}
	opts.lib.ctx = ctx
	defer func() { opts.lib.ctx = nil }()
	return Run(opts)
}

// runContext holds the contexts of a Run
type runContext struct {
	// Cancelled when Run returns or the context of RunContext is done
	ctx    context.Context
	cancel context.CancelFunc

	// For BindBackground functions, also cancelled by 'bg-cancel'
	bgCtx    context.Context
	bgCancel context.CancelFunc
}

// start sets up the contexts of a Run, and makes fzf quit when the context
// of RunContext is done. The returned function releases them.
func (s *libState) start(opts *Options) func() {
	parent := s.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	bgCtx, bgCancel := context.WithCancel(ctx)
	s.mutex.Lock()
	s.run = runContext{ctx, cancel, bgCtx, bgCancel}
	s.mutex.Unlock()

	done := make(chan struct{})
	restoreInput := func() {}
	if s.ctx != nil && opts.Input != nil {
		// Close the input when ctx is done. Otherwise --sync would keep
		// waiting for the end of the input before the interface can quit.
		input := opts.Input
		opts.Input = proxyInput(ctx, input)
		restoreInput = func() { opts.Input = input }
	}
	if s.ctx != nil {
		go func() {
			select {
			case <-parent.Done():
				s.setError(parent.Err())
				s.mutex.Lock()
				s.cancelled = true
				t := s.terminal
				s.mutex.Unlock()
				if t != nil {
					t.interrupt()
				}
			case <-done:
			}
		}()
	}
	return func() {
		close(done)
		cancel()
		restoreInput()
	}
}

// proxyInput forwards the items of input until it is closed or ctx is done
func proxyInput(ctx context.Context, input chan string) chan string {
	proxy := make(chan string)
	go func() {
		defer close(proxy)
		for {
			select {
			case item, more := <-input:
				if !more {
					return
				}
				select {
				case proxy <- item:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return proxy
}

// attach is called when the terminal of a Run is created. If the context
// was done before, fzf quits right away.
func (s *libState) attach(t *Terminal) {
	if s == nil {
		return
	}
	s.mutex.Lock()
	s.terminal = t
	cancelled := s.cancelled
	s.mutex.Unlock()
	if cancelled {
		t.interrupt()
	}
}

// interrupt makes fzf quit with ExitInterrupt
func (t *Terminal) interrupt() {
	t.reqBox.Set(reqQuit, nil)
	// Loop handles requests only after the start signal, which Run defers
	// with --sync, --select-1, --exit-0, or --height=~N. Loop takes this one
	// instead, and the later one from Run stays in the buffer.
	select {
	case t.startChan <- fitpad{-1, -1}:
	default:
	}
}

// context returns the context for a Go callback
func (s *libState) context(background bool) context.Context {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.run.ctx == nil {
		return context.Background()
	}
	if background {
		return s.run.bgCtx
	}
	return s.run.ctx
}

// cancelBackground cancels the running BindBackground functions ('bg-cancel')
func (s *libState) cancelBackground() {
	if s == nil {
		return
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.run.ctx == nil {
		return
	}
	s.run.bgCancel()
	s.run.bgCtx, s.run.bgCancel = context.WithCancel(s.run.ctx)
}
