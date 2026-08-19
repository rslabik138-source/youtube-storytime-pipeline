# avatar

Generates ONE portrait image for a scenario script's narrator — not a
series, not a storyboard, one picture for the whole video. Reads exactly
one thing from `scenario`: the bundle directory `gen export --format
bundle` writes (`manifest.json` — `script.txt` is voiceover's concern, not
this module's). No shared database, no direct dependency on the scenario
module.

The single output is a **transparent PNG** (`portrait.png`): a
click-worthy head-and-shoulders shot — warm genuine smile, direct eye
contact with the camera — with its background matted out, so it drops
straight into `compose` as the portrait cutout. The AI models can't emit
true alpha, so the pipeline is generate-then-cut: produce an opaque image
on a simple studio background, then remove the background with **rembg**
(AI matting — cleaner hair edges than a chroma key, and no green screen
needed since it segments the person from any background). The opaque
original is intentionally not kept.

## Prerequisites

- Go 1.26+ (part of the `d:\project\YouTube` go.work workspace)
- A Google AI Studio API key in `GOOGLE_AI_STUDIO_API_KEY` — the same key
  the `scenario` module already uses for text generation. Image
  generation is a separate capability on the same key; verify your plan
  actually has it enabled before running for real.
- **rembg** for the background-removal step (`pip install "rembg[cli]"`
  plus `pip install onnxruntime` for the inference runtime — the `[cli]`
  extra alone does not pull it). First run per model downloads the model
  (`birefnet-portrait` is ~1 GB, cached under `~/.u2net`). Point
  `settings.yaml`'s `cutout.rembg_cmd` at `rembg`/`rembg.exe` — a full path
  if it isn't on PATH (a user-scope Python install on Windows puts it under
  `%LOCALAPPDATA%\Programs\Python\Python3xx\Scripts`). Only needed for real
  generation; `--no-cutout` skips it, and tests don't need it.
- Tests use `internal/imagegen`'s `FakeGenerator` (a real, valid PNG, no
  network call) and never shell out to rembg — no API key, no Python, no
  model needed to run `go test ./...`.

## Background removal (transparent PNG)

After each image is generated, `avatar generate` runs it through rembg
(`internal/cutout`) and writes only the matted, transparent result. Tune
it under `cutout:` in `settings.yaml`:

- `enabled` — default `true`; set `false` (or pass `--no-cutout`) to write
  the raw opaque image instead, handy for eyeballing the generation prompt.
- `rembg_cmd` — the rembg executable (bare name or full path).
- `model` — `birefnet-portrait` (default, best hair edges for a
  head-and-shoulders human shot); `u2net_human_seg` is a lighter option.

rembg segments the subject from any background, so generation deliberately
does **not** use a green screen — a plain, evenly-lit studio background (as
the prompt asks for) gives it the cleanest matte with no chroma spill.

## How the narrator's appearance gets here

`scenario`'s bible-generation step now produces four appearance fields
alongside name/age/etc (`Person.Build/Hair/FaceNote`, plus `Seed.Attire`
resolved from `configs/professions.yaml`'s per-profession `attire`) and
writes them into `manifest.json`'s `narrator` block. avatar reads them
as-is — it does no image-prompt-relevant generation of its own beyond
filling the template.

Scripts exported **before** this existed will have blank
build/hair/face_note/attire in their manifest — `avatar generate` still
works against those, falling back to neutral, grammatically-safe text
("medium build", "an unremarkable, practical hairstyle", ...) instead of
producing a broken prompt with empty gaps.

## Running your first portrait

1. In the `scenario` module, export a finished script's bundle (same
   bundle voiceover reads):

   ```sh
   cd ../scenario
   gen export <script-id> --format bundle --out output/scripts/<script-id>/
   ```

2. Back in `avatar` (default `scenario_bundle_dir` in `settings.yaml`
   assumes this exact sibling layout):

   ```sh
   avatar generate <script-id> --dry-run
   ```

   Builds the prompt with no API call, writes it to
   `output/<script-id>/prompt.txt` — read it before spending anything.

3. Run it for real:

   ```sh
   avatar generate <script-id>
   ```

   Writes a transparent `output/<script-id>/portrait.png` (generated, then
   background-matted with rembg) and `output/<script-id>/meta.json` (prompt,
   provider/model actually used, real cost). Tries Gemini first, falls back
   to Imagen 4 Fast automatically if Gemini fails. Add `--no-cutout` to skip
   the matting and keep the raw opaque image.

4. Not happy with the result? Generate a few candidates to pick from by
   hand instead of one final image:

   ```sh
   avatar generate <script-id> --variants 3
   ```

   Writes `variant-1.png`, `variant-2.png`, `variant-3.png` — `meta.json`'s
   `files` array lists all of them (no single `portrait.png` in this mode).
   Refuses up front (before calling anything) if `variants × cost per
   image` would exceed `max_cost_usd` in `settings.yaml`.

5. Check what a prior run actually cost and used:

   ```sh
   avatar show <script-id>
   ```

## `--provider`

Default: Gemini first, Imagen 4 Fast as an automatic fallback if Gemini
fails. `--provider gemini` or `--provider imagen` pins to exactly one, no
fallback — useful for comparing the two by hand, or when you already know
one is down.

## Model IDs may need updating

`configs/settings.yaml`'s `gemini_model`/`imagen_model` are the single
place image-generation model IDs live. These change as Google ships new
versions — if generation starts failing with a "model not found"-style
error, that's the first thing to check against current Gemini API docs.
`configs/pricing.yaml`'s `$/image` rates are estimates too; verify against
your own Google AI Studio billing dashboard.

## FLUX was deliberately not used

FLUX.1 [dev] requires a separate commercial license for this kind of use —
not used here, per the brief.

## CLI reference

```
avatar generate <id> [--provider gemini|imagen] [--dry-run] [--variants N] [--no-cutout]
avatar show <id>
```

Every command respects `--config-dir` (default `configs`) and
`--prompts-dir` (default `prompts`).

## Development

```sh
make build   # bin/avatar
make test    # go test ./... — FakeGenerator only, no network, no API key needed
make lint    # gofmt + go vet
```
