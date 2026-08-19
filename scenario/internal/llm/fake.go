package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type FakeCall struct {
	Prompt string
	Opts   Options
}

type fakeResult struct {
	resp Response
	err  error
}

// FakeClient is a Client that never touches the network: it pops one
// queued response (or error) per call, in order, and records every call it
// received so tests can assert on what generate/review/continuity sent.
type FakeClient struct {
	Calls []FakeCall
	queue []fakeResult
}

// NewFakeClient queues responses to be returned one per call, in order.
func NewFakeClient(responses ...Response) *FakeClient {
	f := &FakeClient{}
	for _, r := range responses {
		f.queue = append(f.queue, fakeResult{resp: r})
	}
	return f
}

// NewFakeClientFromFixtures queues dir/name's raw contents as Response.Text,
// one file per call, in the order given.
func NewFakeClientFromFixtures(dir string, names ...string) (*FakeClient, error) {
	f := &FakeClient{}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("llm: fixture %s: %w", name, err)
		}
		f.queue = append(f.queue, fakeResult{resp: Response{Text: string(data)}})
	}
	return f, nil
}

// WithError appends an error-returning entry to the end of the queue, e.g.
// to make the last call in a retry sequence fail.
func (f *FakeClient) WithError(err error) *FakeClient {
	f.queue = append(f.queue, fakeResult{err: err})
	return f
}

func (f *FakeClient) Complete(ctx context.Context, prompt string, opts Options) (Response, error) {
	f.Calls = append(f.Calls, FakeCall{Prompt: prompt, Opts: opts})

	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if len(f.queue) == 0 {
		return Response{}, fmt.Errorf("llm: fake client called with an empty queue (call #%d)", len(f.Calls))
	}

	next := f.queue[0]
	f.queue = f.queue[1:]
	if next.err != nil {
		return Response{}, next.err
	}
	return next.resp, nil
}
