package main

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/story"
)

// costFor computes a usage entry's dollar cost from pricing.yaml's rates.
// ok is false when the model has no pricing entry — callers must show
// "unknown", never a silent $0.00 that reads as "this was free."
func costFor(pricing config.Pricing, u story.UsageEntry) (usd float64, ok bool) {
	mp, found := pricing.For(u.Model)
	if !found {
		return 0, false
	}
	usd = float64(u.TokensIn)/1_000_000*mp.InputPerMillion + float64(u.TokensOut)/1_000_000*mp.OutputPerMillion
	return usd, true
}

// printUsageTable writes a role/provider/model breakdown — tokens in/out,
// thinking tokens broken out separately, and estimated cost — to w. This
// is what makes a cost skew (e.g. most of the bill being thinking tokens
// on chapter generation, not review) visible instead of hiding inside one
// aggregate token count.
func printUsageTable(w io.Writer, pricing config.Pricing, usage []story.UsageEntry) {
	if len(usage) == 0 {
		fmt.Fprintln(w, "  (no usage recorded)")
		return
	}

	sorted := make([]story.UsageEntry, len(usage))
	copy(sorted, usage)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Role != sorted[j].Role {
			return sorted[i].Role < sorted[j].Role
		}
		return sorted[i].Model < sorted[j].Model
	})

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ROLE\tPROVIDER\tMODEL\tCALLS\tTOKENS_IN\tTOKENS_OUT\tTHINKING\tCOST")

	var totalIn, totalOut, totalThinking, totalCalls int
	var totalCost float64
	var anyCostKnown, anyCostUnknown bool
	for _, u := range sorted {
		costStr := "?"
		if usd, ok := costFor(pricing, u); ok {
			costStr = fmt.Sprintf("$%.4f", usd)
			totalCost += usd
			anyCostKnown = true
		} else {
			anyCostUnknown = true
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%d\t%s\n",
			u.Role, u.Provider, u.Model, u.Calls, u.TokensIn, u.TokensOut, u.ThinkingTokens, costStr)
		totalIn += u.TokensIn
		totalOut += u.TokensOut
		totalThinking += u.ThinkingTokens
		totalCalls += u.Calls
	}

	totalCostStr := "?"
	if anyCostKnown {
		totalCostStr = fmt.Sprintf("$%.4f", totalCost)
		if anyCostUnknown {
			totalCostStr += " (partial — some models have no pricing.yaml entry)"
		}
	}
	fmt.Fprintf(tw, "TOTAL\t\t\t%d\t%d\t%d\t%d\t%s\n", totalCalls, totalIn, totalOut, totalThinking, totalCostStr)
	tw.Flush()

	printThinkingBreakdown(w, sorted, totalThinking, totalOut)
	printCostByCause(w, pricing, usage)
}

// printCostByCause answers "how much did REPAIR cost versus CREATION" —
// story.CauseInitial is the first-pass sequential generation (and the
// one-time bible/continuity calls), story.CauseRepair is everything spent
// fixing a violation after the fact (continuity fixes, the repetition
// guard's point-fixes and full regenerations, review's weak-chapter
// regeneration). A script that fails validation after many rounds can
// spend most of its budget here without ever becoming sellable — this is
// what makes that visible instead of hiding inside one total.
func printCostByCause(w io.Writer, pricing config.Pricing, usage []story.UsageEntry) {
	if len(usage) == 0 {
		return
	}

	type totals struct {
		calls                int
		cost                 float64
		anyKnown, anyUnknown bool
	}
	byCause := map[string]*totals{}
	var causeOrder []string
	for _, u := range usage {
		t, ok := byCause[u.Cause]
		if !ok {
			t = &totals{}
			byCause[u.Cause] = t
			causeOrder = append(causeOrder, u.Cause)
		}
		t.calls += u.Calls
		if usd, ok := costFor(pricing, u); ok {
			t.cost += usd
			t.anyKnown = true
		} else {
			t.anyUnknown = true
		}
	}
	sort.Strings(causeOrder)

	fmt.Fprintln(w, "\ncost by cause:")
	for _, cause := range causeOrder {
		t := byCause[cause]
		costStr := "?"
		if t.anyKnown {
			costStr = fmt.Sprintf("$%.4f", t.cost)
			if t.anyUnknown {
				costStr += " (partial)"
			}
		}
		fmt.Fprintf(w, "  %-9s %s (%d calls)\n", cause+":", costStr, t.calls)
	}
}

// printThinkingBreakdown prints thinking-token usage as its own line per
// role — not just a column buried in the wide table above — since this is
// the number that actually explains a cost skew (a role spending most of
// its output tokens on hidden reasoning instead of visible text).
func printThinkingBreakdown(w io.Writer, sorted []story.UsageEntry, totalThinking, totalOut int) {
	if totalOut == 0 {
		return
	}

	byRole := map[string]struct{ thinking, out int }{}
	var roleOrder []string
	for _, u := range sorted {
		if _, ok := byRole[u.Role]; !ok {
			roleOrder = append(roleOrder, u.Role)
		}
		e := byRole[u.Role]
		e.thinking += u.ThinkingTokens
		e.out += u.TokensOut
		byRole[u.Role] = e
	}

	fmt.Fprintln(w, "\nthinking tokens by role:")
	for _, role := range roleOrder {
		e := byRole[role]
		pct := 0.0
		if e.out > 0 {
			pct = 100 * float64(e.thinking) / float64(e.out)
		}
		fmt.Fprintf(w, "  %-12s %d (%.0f%% of its output tokens)\n", role+":", e.thinking, pct)
	}
	fmt.Fprintf(w, "  %-12s %d (%.0f%% of output tokens)\n", "total:", totalThinking, 100*float64(totalThinking)/float64(totalOut))
}

// wordsToTokensRatio approximates how many tokens a word of English prose
// costs — used only to sanity-check reported thinking-token figures
// against actual visible text length, never for billing.
const wordsToTokensRatio = 1.35

// checkThinkingDisabled compares the generate role's total output tokens
// against what the actual generated text alone should need. Gemini's
// OpenAI-compat endpoint doesn't populate completion_tokens_details.
// reasoning_tokens the way OpenAI's own API does (confirmed: our own
// ThinkingTokens field reads 0 across a full real run even though the
// model demonstrably still spends real thinking tokens per the native
// generateContent endpoint's usageMetadata.thoughtsTokenCount) — so a
// reported ThinkingTokens of 0 is not trustworthy proof thinking is off.
// Output tokens meaningfully exceeding what the visible text needs is:
// hidden reasoning tokens the OpenAI-compat layer never reported.
func checkThinkingDisabled(w io.Writer, usage []story.UsageEntry, textWords float64) {
	if textWords <= 0 {
		return
	}
	var generateOut int
	for _, u := range usage {
		if u.Role == "generate" {
			generateOut += u.TokensOut
		}
	}
	if generateOut == 0 {
		return
	}

	expected := textWords * wordsToTokensRatio
	if float64(generateOut) <= expected*1.5 {
		return
	}

	fmt.Fprintf(w, "\nWARNING: generate role's output tokens (%d) are %.1fx the estimated text tokens (~%.0f from %.0f words) — thinking is likely NOT fully disabled, even though reported thinking_tokens may read 0. Consider switching that provider to the native Gemini API (native_thinking_api: true) for real thinkingBudget control and accurate reporting instead of the OpenAI-compat reasoning_effort layer.\n",
		generateOut, float64(generateOut)/expected, expected, textWords)
}
