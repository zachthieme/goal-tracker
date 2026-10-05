# Pitch deck

`goal-tracker-pitch.pptx` is a nine-slide deck pitching Goal Tracker to
leadership. Its screenshots and its 20-second check-in recording come from the
real app over the [fake org](../../README.md#seed-a-fake-org), and the two
scripts here rebuild it.

## Rebuilding it

```sh
scripts/scratch-app start
node docs/pitch/capture.mjs "$(scripts/scratch-app url)"   # CHROME=/usr/bin/chromium on Arch
scripts/scratch-app stop
(cd docs/pitch && npm ci)
node docs/pitch/build.mjs
```

It needs Playwright from `e2e/node_modules` (`make e2e`, or `npm ci` in
`e2e/`, installs it), and `ffmpeg` to convert the recording to MP4.

- **`capture.mjs <base-url> [assets-dir]`** drives the app in headless
  Chromium and writes `assets/shots/*.png` and `assets/checkin.mp4`. It
  **writes to the app** (one Check-in, one published Report), so run it only
  against a throwaway copy such as the scratch app. `assets/` isn't committed.
- **`build.mjs [assets-dir] [out.pptx]`** writes the deck from those assets
  with pptxgenjs. The slide text, speaker notes and the theme's colours are
  all in this file.

The scratch app's seed ends on the machine's date, so a rebuilt deck's
screenshots show different dates, and the same Goals.

## Changing it

Edit the text or layout in `build.mjs` and rebuild; to change what a
screenshot shows, edit `capture.mjs`. Open the result in PowerPoint or
LibreOffice and look at every slide: text that fits in one may wrap in the
other. Slide 2's problems and slide 9's ask are generic. Fit them to the
audience before presenting.
