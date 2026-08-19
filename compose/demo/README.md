Not real output — a mixed real/synthetic test render kept on request.

- **Real**: voice.wav + timing.json from voiceover's `884aec31-...`,
  portrait.png from avatar's `4bdf9a40-...` (nurse), the real background
  video from the `background` module, the Go-drawn rounded caption cards
  built from that real narration, the actual ffmpeg composition, real
  `h264_nvenc` hardware encoding.
- **Synthetic**: music (a sine wave), logo, and the portrait "cutout"
  (real portrait.png with an alpha channel added mechanically — not
  actually background-removed; rembg/Python isn't installed on this
  machine, see `../README.md`).

`preview-60s.mp4` — first 60 seconds only (`--preview 60`), ~53s to
render. `preview-thumb.jpg` — a frame at 20.9s, showing a rounded caption
card (right of the portrait, clear of it) and the composited layers.

Delete this folder whenever — it's a one-off demo, not a build artifact
`compose build` produces on its own.
