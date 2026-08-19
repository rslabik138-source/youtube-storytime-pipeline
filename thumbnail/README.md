# thumbnail

Generates a 1280x720 YouTube thumbnail for a scenario script. Three
layers, deliberately not one AI image call:

1. **Portrait** — a real PNG from the channel's standing face library
   (`configs/faces.yaml`), picked by narrator sex/age and rotated so the
   same face doesn't appear twice in a row. Generated once, by hand,
   never per-script.
2. **Text** — an LLM (cheap: `gemini-3.5-flash-lite`) writes 3-5 colored
   setup/humiliation lines plus a red-plated cliffhanger, from the
   scenario bundle's bible facts.
3. **Composition** — plain HTML/CSS, screenshotted by headless Chrome
   (chromedp). No image-generation model ever touches the text — they
   reliably mangle rendered letters; a browser laying out real CSS never
   does.

Reads exactly one thing from `scenario`: the bundle directory `gen export
--format bundle` writes (`manifest.json`'s new `story` block — see
scenario's `internal/export.BundleStory`). No shared database, no direct
dependency on the scenario module.

## Prerequisites

- Go 1.26+ (part of the `d:\project\YouTube` go.work workspace)
- A Google AI Studio API key in `GOOGLE_AI_STUDIO_API_KEY` (the same key
  scenario/avatar use) — text generation only; nothing else in this
  pipeline calls an API.
- A local Chrome, Chromium, or Edge install for chromedp. Tests don't need
  one (`FakeRenderer`); real `thumb generate` does.
- **At least one real portrait PNG in `assets/faces/`** matching an entry
  in `configs/faces.yaml`. Nothing here generates those — see
  `assets/faces/README.md`.

## Face library

`configs/faces.yaml` ships with two placeholder entries (`face-01`
female, `face-02` male) whose PNG files don't exist yet — `thumb
generate` fails with a clear "no such file" error until you add them.
Requirements for each portrait: smiling, looking at camera, waist-up
framing, soft natural light, blurred neutral background, no text. The
smile is deliberate: it's the contrast against the thumbnail's grim text
that makes the format work as a hook.

## Running your first thumbnail

1. Export a finished script's bundle from `scenario` (same bundle
   voiceover/avatar read) — needs a `scenario` build from after the
   `story` block was added to `BundleManifest`, otherwise `manifest.json`
   won't have `family_law`/`refrain_phrase`/etc and `thumb` will refuse
   to load it:

   ```sh
   cd ../scenario
   gen export <script-id> --format bundle --out output/scripts/<script-id>/
   ```

2. Back in `thumbnail`, check the prompt before spending anything:

   ```sh
   thumb generate <script-id> --dry-run
   ```

   Writes `output/<script-id>/prompt.txt` — no API call, no browser launch.

3. Run it for real:

   ```sh
   thumb generate <script-id>
   ```

   Picks a face (sex/age-matched, rotation-aware), generates 4 different
   texts (`--variants`, default 4) on that same portrait, and writes
   `output/<script-id>/variant-1.png` … `variant-4.png` plus `meta.json`.
   Pick the best one by eye — that's the point of generating several; this
   is the one place in the pipeline where a human's judgment is cheaper
   than more automation.

4. Pin a specific face instead of automatic rotation:

   ```sh
   thumb generate <script-id> --face face-02
   ```

5. Check what a prior run produced:

   ```sh
   thumb show <script-id>
   ```

## Face rotation

`output/face_history.json` (channel-wide, not per-script) tracks the last
few face IDs used. `thumb generate` excludes the last 2 from automatic
selection, falling back to the full matching set if that would leave zero
candidates (e.g. only one face matches a given sex/age bracket at all).

## Text color rules

Checked mechanically after every LLM call (`internal/textgen.Validate`),
not just requested in the prompt:

- 4 to 6 total lines including the final cliffhanger line
- under 40 words total
- each line's color is one of white/yellow/green/magenta/cyan/red
- at most 3 distinct non-white colors per thumbnail
- the final line is never assigned a color — it always renders on its own
  solid red plate (`#E01B1B`), fixed in `templates/thumbnail.html`

A validation failure retries (up to 3 attempts) with the specific
violations fed back into the prompt — mirrors scenario's own
chapter/bible validate-and-retry pattern.

## Composition details

`templates/thumbnail.html`: text zone left 62% (vertically centered),
portrait right 38% (`object-fit: cover`), bold condensed uppercase with a
5px black text-stroke. Font size auto-shrinks (`window.fitText()`, run by
the renderer via `chromedp.Evaluate` before the screenshot) until every
line fits its allocated height — line lengths vary too much for a fixed
size. The bottom-right 120x40px YouTube reserves for the duration badge
falls entirely inside the portrait's 38% zone, so it's clear of text by
construction, not by any special-cased mask.

The portrait is embedded as a `data:` URI directly in the HTML (see
`render.EncodePortrait`) — the rendered page has zero external file
dependencies once written to its temp file, which matters since chromedp
navigates to it via `file://`.

## `--dry-run`

Renders `prompts/thumbnail.tmpl` against the manifest's story facts and
writes it to `prompt.txt` — no LLM call, no browser launch, matching
avatar's own `--dry-run` convention in this workspace. It does **not**
call the (very cheap) text model first and skip only the render step;
read the prompt, don't spend anything.

## Cost

Text: ~$0.001/variant (estimate in `configs/pricing.yaml` — verify
against your own billing). Portrait: $0 (face library, no API).
Composition: $0 (local Chrome). `max_cost_usd` in `settings.yaml` is
checked before any call — 4 variants over budget refuses up front, not
partway through.

## What's been verified for real, and what hasn't

Honest status as of this module's first build:

- **Composition pipeline (HTML → chromedp → PNG): verified for real**
  against a local Chrome install, using synthetic text and a stub
  portrait — the CSS layout, font auto-fit script, and screenshot capture
  all work as designed. See `internal/render/chromedp_real_test.go`
  (skipped by default; `THUMB_REAL_CHROME_TEST=1 go test ./internal/render/...`
  to run it against your own Chrome).
- **Text generation against the real Gemini API: NOT verified.** The
  configured `GOOGLE_AI_STUDIO_API_KEY`'s prepayment credits were
  depleted at the time this module was built (a real 429 from Google,
  not a bug here) — this blocks every API call on that key, scenario's
  own generation included, not just this module. `internal/textgen`'s
  request/response handling is only verified against a fake HTTP server
  (`internal/textgen/client_test.go`) standing in for the real shape.
  Re-run `thumb generate` for real once the key has credits, and fix
  anything `gemini.go`'s assumed shape gets wrong the same way avatar's
  Gemini image client was verified earlier.
- **Face-library bootstrap portraits: NOT generated.** Blocked by the
  same depleted credits — `assets/faces/` is empty. See its own README.

## CLI reference

```
thumb generate <id> [--variants N] [--face <id>] [--dry-run]
thumb show <id>
```

Every command respects `--config-dir` (default `configs`),
`--prompts-dir` (default `prompts`), and `--templates-dir` (default
`templates`).

## Development

```sh
make build   # bin/thumb
make test    # go test ./... — fakes only, no network, no browser needed
make lint    # gofmt + go vet
```
