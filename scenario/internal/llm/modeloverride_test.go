package llm

import (
	"context"
	"testing"
)

func TestWithRoleModelOverridePicksModelForRole(t *testing.T) {
	inner := &stepClient{resp: Response{Text: "ok"}}
	c := WithRoleModelOverride(inner, map[Role]string{
		RoleGenerate: "gemini-3.6-flash",
		RoleSummary:  "gemini-3.5-flash-lite",
	})

	if _, err := c.Complete(context.Background(), "hi", Options{Model: "whatever", Role: RoleSummary}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if inner.lastOpts.Model != "gemini-3.5-flash-lite" {
		t.Fatalf("expected the summary role's model to win, got %q", inner.lastOpts.Model)
	}

	if _, err := c.Complete(context.Background(), "hi", Options{Model: "whatever", Role: RoleGenerate}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if inner.lastOpts.Model != "gemini-3.6-flash" {
		t.Fatalf("expected the generate role's model to win, got %q", inner.lastOpts.Model)
	}
}

func TestWithRoleModelOverrideFallsThroughForUnmappedRole(t *testing.T) {
	inner := &stepClient{resp: Response{Text: "ok"}}
	c := WithRoleModelOverride(inner, map[Role]string{RoleGenerate: "gemini-3.6-flash"})

	if _, err := c.Complete(context.Background(), "hi", Options{Model: "requested-model", Role: RoleReview}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if inner.lastOpts.Model != "requested-model" {
		t.Fatalf("expected the caller's original model to survive for an unmapped role, got %q", inner.lastOpts.Model)
	}
}

func TestWithRoleModelOverrideRespectsForceModel(t *testing.T) {
	inner := &stepClient{resp: Response{Text: "ok"}}
	c := WithRoleModelOverride(inner, map[Role]string{RoleGenerate: "gemini-3.5-flash-lite"})

	_, err := c.Complete(context.Background(), "hi", Options{
		Model: "gemini-3.6-flash", ForceModel: "gemini-3.6-flash", Role: RoleGenerate,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if inner.lastOpts.Model != "gemini-3.6-flash" {
		t.Fatalf("expected ForceModel to win over the role's mapped model, got %q", inner.lastOpts.Model)
	}
}

func TestWithRoleModelOverrideEmptyMapIsNoOp(t *testing.T) {
	inner := &stepClient{resp: Response{Text: "ok"}}
	c := WithRoleModelOverride(inner, nil)
	if c != Client(inner) {
		t.Fatalf("expected WithRoleModelOverride(c, nil) to return c unchanged")
	}
}
