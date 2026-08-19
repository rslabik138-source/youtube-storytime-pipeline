# compose

Final video assembly: background/voiceover/avatar's outputs, composited
via ffmpeg into `compose/output/<id>/final.mp4` — 1920x1080, H.264, 30fps.
No shared database, no Go imports across module boundaries; compose only
ever reads the files listed below.

## Pipeline

1. **Portrait prep** — avatar's `portrait.png` background-removed via the
   `rembg` CLI (Python, local, free), cached by id so a second
   `compose build` on the same id never re-runs it (skipped automatically
   if the cached cutout is already newer than the source portrait).
2. **Captions** — rounded, near-opaque caption cards drawn in Go
   (`internal/subtitles/card.go`, via fogleman/gg) from voiceover's
   `timing.json`, grouped 4-7 words per line (always breaking at a
   sentence boundary even below the minimum) and overlaid by ffmpeg. NOT
   libass — it can't round a box's corners; the cards are rendered as
   full-frame transparent PNGs and burned in via a single concat-demuxer
   overlay (see internal/subtitles/track.go).
3. **Waveform** — drawn live from the real voice track via ffmpeg's
   `showwaves` — sync is automatic because it's literally the same audio
   that plays, not something computed separately.
4. **Music** — a royalty-free ambient track, looped, sidechain-ducked
   under the voice.
5. **Composite** — background (blurred, darkened, vignetted) → portrait
   cutout → waveform → subtitles → logo, in that order, then encoded with
   `h264_nvenc` (falling back to `libx264` automatically if NVENC fails
   for any reason).

## Two real gaps found while building this — read before using

**Kokoro/voiceover has no word-level timing.** The spec calls for
subtitles from "Kokoro's per-word timed words," but the real
`timing.json` (checked against an actual voiceover run this session) only
has **chunk-level** timestamps — one chunk is a whole TTS-request block,
commonly several sentences and tens of seconds (see
`voiceover/internal/assemble.ChunkTiming`'s own doc comment: this is
explicitly the finest granularity that pipeline produces; Kokoro reports
no alignment finer than that). `internal/subtitles/interpolate.go`
estimates per-word timing by distributing each chunk's real, measured
duration across its words proportional to word length — accurate at
chunk boundaries, approximate in between. Real per-word ASR/TTS alignment
would be a genuine improvement if it's ever worth adding upstream in
voiceover; this is the honest fallback for what exists today.

**`rembg` (Python) is not installed on this machine.** Stage 0 needs
`pip install rembg`, and neither Python nor pip resolved to a real
interpreter here (just the Windows Store stub). `internal/rembg` is
built and unit-tested against a fake, and gives a clear
"install it with `pip install rembg`" error if the real CLI isn't found —
but the real rembg call itself has never run. Real end-to-end verification
(below) worked around this by pre-seeding the cutout cache directly.

Two smaller mismatches, already made configurable rather than silently
assumed away:
- avatar's CLI writes `portrait.png`, not `portrait-video.png` as
  written in the original brief — `settings.yaml`'s `portrait_file`
  defaults to the real name.
- `background`'s actual output is a single timestamped file at its own
  module root (`background_2026-...mp4`), not
  `background/output/<id>/bg.mp4` — that module doesn't yet support
  per-id output at all. `settings.yaml`'s `background_dir`/`background_file`
  are fully configurable for whenever that changes; until then, you'd
  need to place (or symlink) a script's background at the expected path
  yourself.

## What's been verified for real

The full ffmpeg pipeline — filter graph, NVENC hardware encoding, all six
composited layers, audio ducking — was run for real against synthetic
test assets (a color-bar test pattern, sine-wave "voice" and "music"
tracks, solid-color stand-ins for the portrait cutout and logo), since no
real background video or rembg output exists yet to test against. The
result: a real 1920x1080/H.264/AAC, exactly voice-length `final.mp4`,
encoded with real `h264_nvenc` hardware encoding on this machine's GTX
1660 Ti — background blur+vignette, the portrait rectangle, a faint but
real `showwaves` trace (subtle because a constant-amplitude sine wave
doesn't vary much — real voice audio will look far more dynamic), burned-
in subtitles in the correct style, and the logo, all in the right
positions. Every other package (`config`, `subtitles`, `rembg`'s caching
logic, `ffmpeg`'s filter-graph/command construction, the NVENC→libx264
fallback) is separately unit-tested against fakes — see `make test`.

## Prerequisites

- Go 1.26+ (part of the `d:\project\YouTube` go.work workspace)
- `ffmpeg` and `ffprobe` on PATH (this machine has a full build with
  `h264_nvenc` and `libx264` — confirmed; captions are drawn in Go and
  overlaid, so no `libass` dependency)
- A caption font: `layout.yaml`'s `subtitles.font_file` may point at a
  .ttf (e.g. Montserrat SemiBold); left blank it uses an embedded bold
  sans-serif, so a render never fails for a missing font
- `rembg` on PATH — `pip install rembg` (requires a real Python install;
  see the gap noted above)
- A real background video, a royalty-free music track with its license
  documented in `settings.yaml`'s `music_license_note`, and a logo PNG —
  none of these exist yet; see `assets/README.md`

## Running a build

```sh
compose build <id>
```

Reads `background/output/<id>/bg.mp4`, `voiceover/output/<id>/{voice.wav,timing.json}`,
`avatar/output/<id>/portrait.png` (paths configurable in `settings.yaml`),
writes `compose/output/<id>/final.mp4` + `subs.ass`.

```sh
compose build <id> --preview 60   # first 60s only, for checking the composition fast
compose build <id> --no-music     # skip music entirely, no license/file required
compose build <id> --no-subs      # skip burning in subtitles
```

## Configuration

- `configs/settings.yaml` — input/output paths, encoder choice, external
  tool commands.
- `configs/layout.yaml` — every position, size, color, and opacity the
  composition uses. Retune the look here, never in Go code.

## CLI reference

```
compose build <id> [--no-music] [--no-subs] [--preview N]
```

Respects `--config-dir` (default `configs`).

## Development

```sh
make build   # bin/compose
make test    # go test ./... — fakes only, no ffmpeg/rembg process needed
make lint    # gofmt + go vet
```
