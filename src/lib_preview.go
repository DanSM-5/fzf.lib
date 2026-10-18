package fzf

// fzf.lib: Go functions rendering the preview window

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/DanSM-5/fzf.lib/src/util"
)

// PreviewFunc renders the preview by writing to w. The output is processed
// like the output of a --preview command: ANSI colors are supported, and it
// is displayed while it is being written.
//
// ctx is cancelled when the preview is no longer needed, e.g. when the cursor
// moves to another item or fzf quits, and when the context given to
// RunContext is done; long-running functions should return then. A
// returned error, or a panic, is displayed in the preview window.
type PreviewFunc func(ctx context.Context, state *State, w io.Writer) error

// PreviewRefresh controls when a PreviewFunc is called again
type PreviewRefresh int

const (
	// PreviewOnFocus updates the preview when the current item or the
	// selection changes, like a --preview command with {+}
	PreviewOnFocus PreviewRefresh = iota

	// PreviewOnQuery also updates the preview when the query changes, and
	// when there is no match, like a --preview command with {q}
	PreviewOnQuery
)

// The preview "command" of a PreviewFunc. Keeping a command template lets
// every upstream check for an active preview work unchanged; the placeholders
// define when fzf refreshes the preview. The NUL byte keeps it from ever
// matching a real command.
const libPreviewPrefix = "\x00fzf.lib:preview"

// SetPreviewFunc renders the preview window with fn instead of a --preview
// command. The window is configured with --preview-window as usual, and the
// preview actions (toggle-preview, refresh-preview, preview-down, ...) work
// as with a command. 'change-preview(...)' and 'preview(...)' switch back to
// a shell command.
func (o *Options) SetPreviewFunc(fn PreviewFunc, refresh PreviewRefresh) error {
	if o.lib == nil {
		return errors.New("SetPreviewFunc requires Options created by Parse")
	}
	if fn == nil {
		return errors.New("SetPreviewFunc requires a function")
	}
	template := libPreviewPrefix + " {+}"
	switch refresh {
	case PreviewOnFocus:
	case PreviewOnQuery:
		template += " {q}"
	default:
		return fmt.Errorf("invalid preview refresh mode: %d", refresh)
	}
	o.lib.preview = fn
	o.Preview.command = template
	return nil
}

// previewProcess runs either a --preview command or a PreviewFunc, with the
// subset of the exec.Cmd interface used by the preview loop
type previewProcess struct {
	cmd *exec.Cmd

	fn     func(ctx context.Context, w io.Writer) error
	reader *io.PipeReader
	writer *io.PipeWriter
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func (t *Terminal) newPreviewProcess(template string, command string, env []string, items [3][]*Item) *previewProcess {
	if t.lib == nil || t.lib.preview == nil || !strings.HasPrefix(template, libPreviewPrefix) {
		cmd := t.executor.ExecCommand(command, true)
		cmd.Env = env
		return &previewProcess{cmd: cmd}
	}

	var current *Item
	if len(items[0]) > 0 {
		current = items[0][0]
	}
	state := t.newLibState(env, current, items[1])
	preview := t.lib.preview
	p := &previewProcess{
		fn:   func(ctx context.Context, w io.Writer) error { return preview(ctx, state, w) },
		done: make(chan struct{}),
	}
	p.reader, p.writer = io.Pipe()
	p.ctx, p.cancel = context.WithCancel(t.lib.context(false))
	return p
}

// StdoutPipe returns the output of the preview. For a command, stderr is
// merged into it.
func (p *previewProcess) StdoutPipe() (io.ReadCloser, error) {
	if p.cmd != nil {
		out, err := p.cmd.StdoutPipe()
		p.cmd.Stderr = p.cmd.Stdout
		return out, err
	}
	return p.reader, nil
}

func (p *previewProcess) Start() error {
	if p.cmd != nil {
		return p.cmd.Start()
	}
	go func() {
		defer close(p.done)
		err := p.call()
		if err != nil && p.ctx.Err() == nil {
			fmt.Fprintln(p.writer, err.Error())
		}
		p.writer.Close()
	}()
	return nil
}

func (p *previewProcess) call() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in preview function: %v", r)
		}
	}()
	return p.fn(p.ctx, p.writer)
}

// Wait waits for the preview to finish. A cancelled PreviewFunc is not waited
// for, so that one ignoring ctx cannot stall the preview loop.
func (p *previewProcess) Wait() error {
	if p.cmd != nil {
		return p.cmd.Wait()
	}
	defer p.cancel()
	select {
	case <-p.done:
	case <-p.ctx.Done():
	}
	return nil
}

func (p *previewProcess) Kill() {
	if p.cmd != nil {
		util.KillCommand(p.cmd)
		return
	}
	p.cancel()
	// Unblock the reader even if the function keeps running, and make its
	// further writes fail
	p.reader.CloseWithError(context.Canceled)
}
