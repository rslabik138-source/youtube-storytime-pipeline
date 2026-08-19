package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type alwaysClient struct {
	resp Response
	err  error
}

func (a alwaysClient) Complete(ctx context.Context, prompt string, opts Options) (Response, error) {
	if a.err != nil {
		return Response{}, a.err
	}
	return a.resp, nil
}

func TestWithFailoverUsesFirstProviderWhenItSucceeds(t *testing.T) {
	first := alwaysClient{resp: Response{Text: "from first"}}
	second := alwaysClient{err: errors.New("should never be called")}

	c := WithFailover(nil, Named{Name: "first", Client: first}, Named{Name: "second", Client: second})
	resp, err := c.Complete(context.Background(), "hi", Options{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "from first" {
		t.Fatalf("expected response from first provider, got %q", resp.Text)
	}
}

func TestWithFailoverMovesToNextProviderOnError(t *testing.T) {
	first := alwaysClient{err: errors.New("first is down")}
	second := alwaysClient{resp: Response{Text: "from second"}}

	c := WithFailover(nil, Named{Name: "first", Client: first}, Named{Name: "second", Client: second})
	resp, err := c.Complete(context.Background(), "hi", Options{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "from second" {
		t.Fatalf("expected response from second provider, got %q", resp.Text)
	}
}

func TestWithFailoverReturnsCombinedErrorWhenAllFail(t *testing.T) {
	first := alwaysClient{err: errors.New("first is down")}
	second := alwaysClient{err: errors.New("second is down too")}

	c := WithFailover(nil, Named{Name: "first", Client: first}, Named{Name: "second", Client: second})
	_, err := c.Complete(context.Background(), "hi", Options{})
	if err == nil {
		t.Fatalf("expected an error when every provider fails")
	}
	if !strings.Contains(err.Error(), "second is down too") {
		t.Fatalf("expected the combined error to mention the last provider's failure, got %v", err)
	}
}

func TestWithFailoverEmptyProviderList(t *testing.T) {
	c := WithFailover(nil)
	if _, err := c.Complete(context.Background(), "hi", Options{}); err == nil {
		t.Fatalf("expected an error for an empty provider list")
	}
}

func TestWithFailoverStopsImmediatelyOnModelNotFound(t *testing.T) {
	first := alwaysClient{err: ErrModelNotFound}
	second := alwaysClient{resp: Response{Text: "should not be reached"}}

	c := WithFailover(nil, Named{Name: "first", Client: first}, Named{Name: "second", Client: second})
	_, err := c.Complete(context.Background(), "hi", Options{})
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound to surface, got %v", err)
	}
	if strings.Contains(err.Error(), "should not be reached") {
		t.Fatalf("expected failover to stop at the first provider's 404, not try the second")
	}
}

func TestWithFailoverSkipsProviderThatDoesNotSupportTheRole(t *testing.T) {
	generateOnly := alwaysClient{err: errors.New("should never be called for summary")}
	summaryCapable := alwaysClient{resp: Response{Text: "from summary-capable backup"}}

	c := WithFailover(nil,
		Named{Name: "generate-only", Client: generateOnly, Roles: []Role{RoleGenerate}},
		Named{Name: "summary-capable", Client: summaryCapable, Roles: []Role{RoleSummary, RoleReview}},
	)
	resp, err := c.Complete(context.Background(), "hi", Options{Role: RoleSummary})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "from summary-capable backup" {
		t.Fatalf("expected the role-eligible provider's response, got %q", resp.Text)
	}
}

func TestWithFailoverSkipsProviderWhoseCapIsTooSmallForTheRequest(t *testing.T) {
	tooSmall := alwaysClient{err: errors.New("should never be called, request exceeds its cap")}
	roomy := alwaysClient{resp: Response{Text: "from the uncapped provider"}}

	c := WithFailover(nil,
		Named{Name: "small-cap", Client: tooSmall, MaxTokensPerRequest: 10},
		Named{Name: "roomy", Client: roomy},
	)
	resp, err := c.Complete(context.Background(), "a reasonably long prompt that exceeds ten tokens of budget", Options{MaxTokens: 500})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "from the uncapped provider" {
		t.Fatalf("expected the uncapped provider's response, got %q", resp.Text)
	}
}

func TestWithFailoverNoEligibleProviderReturnsClearError(t *testing.T) {
	unreachable := alwaysClient{err: errors.New("should never be called")}

	c := WithFailover(nil, Named{Name: "summary-only", Client: unreachable, Roles: []Role{RoleSummary}})
	_, err := c.Complete(context.Background(), "hi", Options{Role: RoleGenerate})
	if err == nil {
		t.Fatalf("expected an error when no provider supports the role")
	}
	if !strings.Contains(err.Error(), "generate") {
		t.Fatalf("expected the error to name the unsupported role, got %v", err)
	}
}

func TestWithFailoverStopsOnContextCancellation(t *testing.T) {
	first := alwaysClient{err: context.Canceled}
	second := alwaysClient{resp: Response{Text: "should not be reached"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := WithFailover(nil, Named{Name: "first", Client: first}, Named{Name: "second", Client: second})
	_, err := c.Complete(ctx, "hi", Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
