# Face library images

Empty on purpose. Put the channel's standing portraits here — 3-5 PNGs,
generated once by hand (or via any image tool), matching the IDs listed in
`configs/faces.yaml`.

Requirements for each portrait (see `configs/faces.yaml`'s own comment for
the full brief): smiling, looking at camera, waist-up, soft natural light,
blurred neutral background, no text. The smile is deliberate — it's the
contrast against the thumbnail's grim text that makes the format work.

`configs/faces.yaml` currently expects:
- `face-01.png` — female, reads as roughly 35-55
- `face-02.png` — male, reads as roughly 40-60

Neither file exists yet. `thumb generate` will fail with a clear "read face
portrait ... no such file" error until at least one matching your
narrator's sex/age is added.
