package llm

import (
	"context"
	"testing"
)

func TestWithThinkingBudgetSetsReasoningEffortForConfiguredRole(t *testing.T) {
	inner := &stepClient{resp: Response{Text: "ok"}}
	c := WithThinkingBudget(inner, map[Role]int{RoleGenerate: 0, RoleContinuity: 1024, RoleReview: 512})

	if _, err := c.Complete(context.Background(), "hi", Options{Role: RoleGenerate}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if inner.lastOpts.ReasoningEffort != "low" {
		t.Fatalf("expected generate's budget 0 to map to \"low\" (not \"none\" — rejected by real models), got %q", inner.lastOpts.ReasoningEffort)
	}
	if !inner.lastOpts.HasThinkingBudget || inner.lastOpts.ThinkingBudgetTokens != 0 {
		t.Fatalf("expected HasThinkingBudget=true and ThinkingBudgetTokens=0 (a real configured zero, not \"unset\"), got %+v", inner.lastOpts)
	}

	if _, err := c.Complete(context.Background(), "hi", Options{Role: RoleContinuity}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if inner.lastOpts.ReasoningEffort != "low" {
		t.Fatalf("expected continuity's budget 1024 to map to \"low\", got %q", inner.lastOpts.ReasoningEffort)
	}

	if _, err := c.Complete(context.Background(), "hi", Options{Role: RoleReview}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if inner.lastOpts.ReasoningEffort != "low" {
		t.Fatalf("expected review's budget 512 to map to \"low\", got %q", inner.lastOpts.ReasoningEffort)
	}
}

func TestReasoningEffortForBuckets(t *testing.T) {
	tests := []struct {
		budget int
		want   string
	}{
		{-1, "low"},
		{0, "low"},
		{1, "low"},
		{2000, "low"},
		{2001, "medium"},
		{12000, "medium"},
		{12001, "high"},
		{50000, "high"},
	}
	for _, tt := range tests {
		if got := reasoningEffortFor(tt.budget); got != tt.want {
			t.Errorf("reasoningEffortFor(%d) = %q, want %q", tt.budget, got, tt.want)
		}
	}
}

func TestWithThinkingBudgetLeavesUnconfiguredRoleAlone(t *testing.T) {
	inner := &stepClient{resp: Response{Text: "ok"}}
	c := WithThinkingBudget(inner, map[Role]int{RoleGenerate: 0})

	if _, err := c.Complete(context.Background(), "hi", Options{Role: RoleReview}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if inner.lastOpts.ReasoningEffort != "" {
		t.Fatalf("expected no ReasoningEffort for an unconfigured role, got %q", inner.lastOpts.ReasoningEffort)
	}
	if inner.lastOpts.HasThinkingBudget {
		t.Fatalf("expected HasThinkingBudget=false for an unconfigured role")
	}
}

func TestWithThinkingBudgetEmptyMapIsNoOp(t *testing.T) {
	inner := &stepClient{resp: Response{Text: "ok"}}
	c := WithThinkingBudget(inner, nil)
	if c != Client(inner) {
		t.Fatalf("expected WithThinkingBudget(c, nil) to return c unchanged")
	}
}
