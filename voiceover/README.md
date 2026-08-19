# voiceover

Synthesizes a finished `scenario` script into a final WAV plus a timing
manifest, via a locally-running Kokoro-FastAPI instance. Reads exactly one
thing from `scenario`: the bundle directory `gen export --format bundle`
writes (`script.txt` + `manifest.json`). No shared database, no direct
dependency on the scenario module.

## Prerequisites

- Go 1.26+ (this repo is part of the `d:\project\YouTube` go.work workspace)
- [ffmpeg](https://ffmpeg.org/download.html) on `PATH` (bundles `ffprobe`) —
  `voice speak` checks for both at startup and fails with a clear message
  if either is missing.
- Docker, for Kokoro-FastAPI (see below). Not required to run the test
  suite — every test uses `internal/kokoro`'s `FakeSynth` or synthetic
  test audio, never a real network call.

## Running Kokoro-FastAPI (Docker, NVIDIA GPU)

Kokoro-FastAPI serves an OpenAI-compatible `/v1/audio/speech` endpoint.
For an NVIDIA GPU (this project targets a 6GB-VRAM laptop GPU, hence
`concurrency: 2` by default in `settings.yaml`):

```sh
docker run --gpus all -p 8880:8880 ghcr.io/remsky/kokoro-fastapi-gpu:latest
```

Confirm it's up:

```sh
curl http://localhost:8880/v1/audio/voices
```

`configs/settings.yaml`'s `kokoro_url` defaults to `http://localhost:8880`
— change it if you're running Kokoro elsewhere (a different port, a remote
host, etc).

## Filling in `configs/voices.yaml`

`configs/voices.yaml` ships with a handful of starter entries using
Kokoro's known voice IDs (`af_*`, `am_*`, `bf_*`, `bm_*`) — but their
`age_feel` ranges and `texture` labels are **placeholders**, not verified
judgments. To do this for real:

1. `voice list-voices` — lists every voice Kokoro currently serves and
   flags (`*`) any not yet described in `voices.yaml`.
2. For each one, `voice sample --voice <id> --text "Some representative
   sentence from an actual script."` — writes a WAV to
   `output/samples/<id>.wav`. Listen to it.
3. Update (or add) that voice's entry in `voices.yaml`: `sex` (as it
   actually reads, not the ID's naming convention), `age_feel` (the age
   range this voice's TEXTURE plausibly fits, not a literal claim about
   the voice actor), `texture` (a short word: warm, measured, weathered,
   neutral, ...).

`internal/catalog.Select` only ever reads from this file — a voice with no
entry here is never chosen automatically (though `--voice <id>` on `speak`
always works regardless, since it bypasses the catalog).

## Running your first voiceover

1. In the `scenario` module, export a finished script's bundle:

   ```sh
   cd ../scenario
   gen export <script-id> --format bundle --out output/scripts/<script-id>/
   ```

2. Back in `voiceover` (default `scenario_bundle_dir` in `settings.yaml`
   assumes this exact sibling layout):

   ```sh
   voice speak <script-id> --dry-run
   ```

   Confirms chunking looks right and gives a rough render-time estimate —
   no Kokoro calls yet.

3. Run it for real:

   ```sh
   voice speak <script-id>
   ```

   Writes `output/<script-id>/voice.wav` and `output/<script-id>/timing.json`,
   auto-selecting a voice from the catalog (narrator sex/age, excluding the
   last 3 voices used) unless you pass `--voice <id>`.

4. Spot-check the seams without listening to the whole thing:

   ```sh
   voice speak <script-id> --sample-seams
   ```

   Renders 3 random chunk-boundary samples (5s before/after each) into
   `output/<script-id>/seams/`.

## `--stitch=builtin|custom`

`custom` (the default) trims silence padding off every synthesized chunk
and crossfades same-paragraph joins before concatenating — this is the
recommended path; concatenating raw WAV chunks directly is the real seam
risk this exists to avoid. `builtin` skips all of that (plain concatenation
plus the configured pause silence only) as a comparison baseline — if it
sounds just as good on your setup, that's a real option, but verify by ear
with `--sample-seams` before trusting it for a full run.

## CLI reference

```
voice list-voices                          # diff Kokoro's live voices against voices.yaml
voice sample --voice af_bella --text "..."  # one short piece, for listening
voice speak <id> [--voice X] [--speed 1.0] [--dry-run] [--stitch=builtin|custom] [--sample-seams]
voice show <id>                            # voice, duration, size for one voiceover
voice stats                                # totals across every recorded voiceover
```

Every command respects `--config-dir` (default `configs`) and `--db`
(overrides `settings.yaml`'s `db_path`).

## Development

```sh
make build   # bin/voice
make test    # go test ./... — real ffmpeg/ffprobe, no Docker, no network
make lint    # gofmt + go vet
```
