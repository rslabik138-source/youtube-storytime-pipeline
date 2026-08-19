# assets

Empty on purpose — put your own here:

- `logo.png` — the channel logo, overlaid top-left. Any size; it's scaled
  to `layout.yaml`'s `logo.height` (60px by default) with aspect preserved.
- `music/` — royalty-free ambient background tracks. Point
  `settings.yaml`'s `music_file` at the one to use, and fill in
  `music_license_note` with where its license lives (a URL, a receipt
  path, "CC0") — `compose build` refuses to run with music enabled and
  that note left blank.

Neither file exists yet. `compose build` fails with a clear "missing"
error for the logo; music is skipped cleanly with `--no-music`.
