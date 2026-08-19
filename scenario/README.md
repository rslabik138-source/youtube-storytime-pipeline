# underestimated — long-form script generator

Generates full-length (~8,500 word, ~55 minute) English-language YouTube
scripts for the "underestimated" channel: first-person "calm drama / quiet
revenge" stories where an undervalued profession's habit of mind produces
the record that, years later, quietly exposes a family lie.

## Quickstart

```sh
# 1. Set at least one provider's API key (never put keys in config or code).
export GOOGLE_AI_STUDIO_API_KEY=...      # primary provider (see configs/settings.yaml)
export GROQ_API_KEY=...                  # backup provider

# 2. Build
go build -o bin/gen ./cmd/gen            # or: make build

# 3. Generate a script
./bin/gen generate

# 4. See what you got
./bin/gen list
./bin/gen show <id>
./bin/gen export <id> --format tts --out output/tts/<id>.txt
```

On Windows without `make`, run the `go build`/`go test`/`go vet` commands
from the Makefile directly.

## Commands

| Command | Purpose |
|---|---|
| `gen generate [--dry-run] [--profession X] [--seed N] [--provider NAME]` | Run the full pipeline: seed → bible → 16 chapters → continuity → script validation → review → save. `--dry-run` stops after the bible (no chapter/continuity/review LLM calls — a $0 diversity check). |
| `gen generate --resume [--provider NAME]` | Continue the most recent pending script from wherever chapter generation, continuity, or script validation left off — every chapter already generated is saved after it completes, so a provider outage or rate limit mid-script doesn't lose that work. Can't be combined with `--dry-run`, `--profession`, or `--seed`. |
| `gen list [--status accepted\|rejected\|pending] [--limit N]` | List stored scripts. |
| `gen show <id> [--chapter N]` | Show a script's metadata, or one chapter's text. |
| `gen regenerate <id> --chapter N [--provider NAME]` | Regenerate a single chapter in place. |
| `gen export <id> --format tts\|timing\|meta [--out PATH] [--ssml]` | Write a TTS transcript, a per-chapter/per-paragraph timing manifest, or YouTube upload metadata. `tts` is plain text by default (for engines like Kokoro that don't support SSML) — pass `--ssml` for `<break>` tags instead. Either way, `timing`'s `pause_after_ms` field carries the actual pacing. |
| `gen stats` | Aggregate counts and averages across every stored script, plus a cost/token/thinking-token breakdown by role (generate/summary/review/continuity) and model — this is what surfaces a cost skew (e.g. most of the bill being thinking tokens on chapter generation) instead of hiding it in one total. Also warns if generate's output tokens look inflated relative to the actual text (see "Thinking-token visibility" below). |
| `gen validate <id>` | Run every validator (bible, per-chapter, whole-script) against an already-saved script — zero LLM calls. Calibrate a validator threshold and see its effect on real, already-generated text for $0, instead of through a full paid run. |

Global flags: `--config-dir` (default `configs`), `--prompts-dir` (default
`prompts`), `--db` (overrides `settings.yaml`'s `db_path`).

## Configuration

Everything that shapes a story — professions, axis values, the chapter
structure, name pools, banned phrases, and provider/model settings — lives
in `configs/*.yaml`, not in Go code:

- `professions.yaml` — each profession's epistemology, record type, and the
  betrayal values that record type can expose.
- `axes.yaml` — antagonist, weak_ally, humiliation_type, betrayal (the full
  catalog), duration (short/long), written_overreach, object_container,
  legacy_artifact, reckoning_place (private/public), ending_type,
  protagonist_sex. Any weighted value can carry a `weight`.
- `chapters.yaml` — the 16-beat structure with `target_words` measured
  against real competitor scripts; don't change the numbers without
  re-measuring. `split: true` on a beat (currently `the_years` and
  `the_reckoning`, the two over 900 words) generates it as two sequential
  half-length calls instead of one — the second gets the first's finished
  text as context. Smaller individual requests are less exposed to a
  provider's "high demand" rejection on the largest beats, and a failure on
  either half only costs that half's regeneration, not the whole chapter.
- `names.yaml` — regional name pools (first/last names + towns), split by
  generation (`young`, `old`), so the model gets an allowed list instead of
  inventing names freely.
- `banned_phrases.yaml` — stock AI-narration tells to reject mechanically.
- `settings.yaml` — providers (in failover order, each with its own
  per-role `models` map — see below), target word count / wpm, quality
  threshold, retry limits, rate limiting, `thinking_budget` per role, DB
  and output paths.
- `pricing.yaml` (optional) — $/million-token rates per model, used only to
  compute the cost column in `gen stats` / `gen generate`'s usage table. A
  model with no entry there shows `?` for cost rather than a silent,
  misleading $0.00. Missing the whole file is fine too — costs just show
  as unknown everywhere.

**API keys live only in environment variables**, named by each provider's
`api_key_env` in `settings.yaml` — never in a config file or in Go code.

### Per-role models and thinking budget

Each provider's `models` map picks its OWN model per role — not one shared
model per provider:

```yaml
providers:
  - name: google-ai-studio
    base_url: https://generativelanguage.googleapis.com/v1beta/openai
    api_key_env: GOOGLE_AI_STUDIO_API_KEY
    supports_thinking_config: true # Gemini-family — understands thinking_budget
    models:
      generate: gemini-3.6-flash
      summary: gemini-3.5-flash-lite
      review: gemini-3.5-flash-lite
      continuity: gemini-3.6-flash

thinking_budget:
  generate: 1 # chapter prose needs no real reasoning — 1, not 0 (see below)
  summary: 1
  review: 512
  continuity: 1024
```

This matters specifically because failing over to a backup provider must
not collapse the generate/summary/review split onto one model — each
provider declares its own mapping instead. `thinking_budget` is only sent
to providers with `supports_thinking_config: true` (Groq etc. ignore it
entirely); how it's sent depends on `native_thinking_api` (see below).

**Thinking-token visibility.** A provider with `native_thinking_api: true`
(google-ai-studio) goes through Gemini's own `generateContent` endpoint
instead of the OpenAI-compatible one, sending `thinking_budget`'s value
as-is to `generationConfig.thinkingConfig.thinkingBudget`. This exists
because of a confirmed, real-API discrepancy: Gemini's OpenAI-compat
endpoint accepts `reasoning_effort` (bucketed from `thinking_budget`'s
number into none/low/medium/high) but its responses never populate
`usage.completion_tokens_details.reasoning_tokens` — so `ThinkingTokens`
always read 0 through that path even on a run where output tokens were
measurably inflated by real, billed reasoning. The native endpoint's
`usageMetadata.thoughtsTokenCount` does report it accurately. `gen stats`
/ `gen generate` both run this check automatically (`checkThinkingDisabled`
in `cmd/gen/usage.go`): if generate role's total output tokens exceed
1.5x what the actual generated text should need, it prints a warning
recommending `native_thinking_api: true` for that provider — that's
literally how this repo's own google-ai-studio provider ended up switched
(a real run showed a 4.8x ratio).

Two more things confirmed against the real API, both still true after
switching to the native endpoint: (1) `gemini-3.6-flash` rejects a
thinking budget/effort of exactly 0 outright with a 400 — this model
family doesn't accept fully-disabled reasoning. The OpenAI-compat path
silently bucketed 0 up to `"low"`; the native path does NOT auto-adjust,
so `thinking_budget` must be >= 1 for any `native_thinking_api` provider,
or every call 400s. (2) Even at the lowest accepted values (1, 128, and
512 all tested), `gemini-3.6-flash` still spends roughly 180-220 real
thinking tokens per call regardless — a hard per-call floor for this
model that no client-side budget value can get under. Every LLM call logs
its real `thinking_tokens` (`"llm call usage"` at info level) so this is
visible live, per call, not just in the aggregate `gen stats` view.

### Adding a profession (no code changes)

Add an entry to `configs/professions.yaml`:

```yaml
- name: pharmacist
  epistemology: you learn to read what a prescription history says about a family
  record_type: the dispensing log
  exposes: [medication_diverted, insurance_fraud]
```

`exposes` values must already exist in `configs/axes.yaml`'s `betrayal`
list — that's the hard pairing constraint `internal/seed` enforces: a
script's betrayal is only ever drawn from its own profession's `exposes`,
never freely from the full catalog. Add new betrayal values to `axes.yaml`
first if the one you want doesn't exist yet.

### Adding a provider (no code changes)

Add an entry to `configs/settings.yaml`'s `providers` list — any
OpenAI-compatible chat completions endpoint works:

```yaml
- name: cerebras
  base_url: https://api.cerebras.ai/v1
  api_key_env: CEREBRAS_API_KEY
  models:
    generate: llama-3.3-70b
    summary: llama-3.3-70b
    review: llama-3.3-70b
    continuity: llama-3.3-70b
```

Providers are tried in the order they're listed; each is retried with
backoff before failover moves to the next one.

A backup provider with a low per-request token quota (e.g. Groq's free
tier) shouldn't be offered the biggest calls (chapter generation,
continuity's whole-script check) — it'll just 413. Restrict it with
`roles` (the only roles failover will ever route to it; omit for "all
roles") and `max_tokens_per_request` (failover skips it, rather than
sending and failing, once the prompt + requested completion tokens are
estimated to exceed this):

```yaml
- name: groq
  base_url: https://api.groq.com/openai/v1
  api_key_env: GROQ_API_KEY
  roles: [summary, review]
  max_tokens_per_request: 12000
  models:
    summary: llama-3.3-70b-versatile
    review: llama-3.3-70b-versatile
```

If no configured provider supports a role at all, failover fails
immediately with a clear error instead of attempting (and losing to) a
request that was never going to fit.

## Architecture

```
cmd/gen              cobra CLI — wires config, templates, store, and the
                      LLM client together; no pipeline logic lives here
internal/story        domain types (Seed, Bible, Chapter, Script,
                       QualityScores) + mechanical validators. BLOCKING
                       (forces a fix): the hard 28-word sentence cap
                       (warning from 26, matching the prompt's own 20-25
                       word rhythm sentence so that instruction and
                       validator agree), banned phrases, TTS digits,
                       beat-specific checks (the_cut direct speech, the_
                       demand written document), and whole-script anti-
                       repetition (6-word phrases, sentence openings,
                       refrain-phrase placement, exact money amounts —
                       internal/generate's script-validation step).
                       WARNING only (logged, no fix triggered): chapter
                       word-count ±20%, and sentence-length variance
                       (needs >= 2 sentences of 20+ words) — both are
                       style signals worth a human's attention, not
                       correctness bugs worth burning a retry over
internal/config        loads and validates the six YAML config files
internal/llm            Client interface + OpenAI-compat implementation,
                        retry (explicit backoff schedule with jitter, 429
                        and 5xx only — a 404 is a config error and skips
                        both retry and failover), failover, rate limiting,
                        and a fake for tests
internal/seed          draws a Seed: hard profession→betrayal pairing,
                       dedup against story history, forced alternation on
                       duration and reckoning_place
internal/generate       the orchestrator: bible → chapters (+ separate
                        summary calls, saved progressively so `--resume`
                        can continue after a failure) → continuity →
                        whole-script validation → review → save.
                        Whole-script validation fixes are point-fixes by
                        default: a deterministic pass removes exact
                        duplicate sentences for free, then each remaining
                        violation gets a single small call to rewrite
                        just its one sentence — a chapter only gets fully
                        regenerated (~12,000 tokens) if it has more than 8
                        violations, or a violation that can't be localized
                        to one sentence.
                        settings.yaml's max_cost_usd stops a script's
                        generation (state already saved, nothing lost)
                        once its running cost crosses that many dollars.
                        Every chapter prompt also gets a proactive avoid-
                        list built from chapters 1..N-1 (every paragraph
                        opening already used, every 6-word phrase already
                        used twice, a running count per exact dollar
                        amount mentioned) — ~150 extra input tokens that
                        head off most script-validation rounds before they
                        happen. And if each of the first 3 freshly
                        generated chapters needed more than 5 blocking
                        violations fixed in a single attempt,
                        ErrPromptLikelyBroken stops the whole run
                        immediately: that's a broken prompt, not an
                        unlucky seed, and the other 13 chapters would show
                        the same trouble
internal/continuity     one LLM call comparing the finished script to its
                        bible — including an explicit numeric fact
                        cross-check and title verification (the title is
                        corrected here, after the full text exists, not
                        left as whatever the bible guessed before a single
                        chapter was written) — returns a capped, targeted
                        fix list
internal/review          seven-axis quality scoring + weak-chapter retry
internal/store           SQLite (modernc.org/sqlite, no cgo) + in-memory
                        implementations of the same Store interface
internal/export          tts / timing / meta artifact builders
configs/*.yaml           everything in the "Configuration" section above
prompts/*.tmpl           the six prompts (bible, chapter, summary, review,
                        continuity, pointfix), English only, rendered by
                        the packages above via Go's text/template
```

Every package that calls an LLM depends only on the `llm.Client` interface,
so the entire pipeline (`internal/generate`'s tests included) runs against
a fake client with no network access and no real database.

## Development

```sh
make build   # go build -o bin/gen ./cmd/gen
make test    # go test ./...
make vet     # go vet ./...
make fmt     # gofmt -l -w .
make lint    # go vet, plus golangci-lint if installed
```
