package textgen

import (
	"context"
	"fmt"
)

type fakeResult struct {
	resp Response
	err  error
}

// FakeClient is a Client that never touches the network: it pops one
// queued response (or error) per call, in order, and records every prompt
// it received so tests can assert on what Generate sent.
type FakeClient struct {
	Prompts []string
	queue   []fakeResult
}

// NewFakeClient queues responses to be returned one per call, in order.
func NewFakeClient(responses ...Response) *FakeClient {
	f := &FakeClient{}
	for _, r := range responses {
		f.queue = append(f.queue, fakeResult{resp: r})
	}
	return f
}

// WithError appends an error-returning entry to the end of the queue.
func (f *FakeClient) WithError(err error) *FakeClient {
	f.queue = append(f.queue, fakeResult{err: err})
	return f
}

func (f *FakeClient) Complete(ctx context.Context, prompt, model string) (Response, error) {
	f.Prompts = append(f.Prompts, prompt)

	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if len(f.queue) == 0 {
		return Response{}, fmt.Errorf("textgen: fake client called with an empty queue (call #%d)", len(f.Prompts))
	}
	next := f.queue[0]
	f.queue = f.queue[1:]
	if next.err != nil {
		return Response{}, next.err
	}
	return next.resp, nil
}
