# assets

Images referenced by the README. Nothing here ships in the binary.

## report-card.svg / doctor-card.svg

The terminal cards in the README. Hand-written SVG rather than a
screenshot, so it stays sharp at any size, weighs a few kilobytes, and shows a
readable diff when the report format changes.

Two constraints to know before editing it:

- **Inline attributes only.** GitHub sanitises SVG in Markdown and may drop
  `<style>` blocks, so every fill, font, and size is set as an attribute on the
  element itself.
- **Columns use explicit `x` coordinates, not runs of spaces.** SVG renderers
  collapse whitespace unless `xml:space="preserve"` is honoured, and Chromium
  does not do so reliably. A card built from padded strings loses its alignment
  as soon as it is rendered — the first version of this file did exactly that.

Preview it the way GitHub will render it before committing:

```bash
python -m http.server 8901 --bind 127.0.0.1

# in another shell
npx playwright screenshot --viewport-size=640,610 \
  http://127.0.0.1:8901/assets/report-card.svg /tmp/card.png
```

Keep the content in sync with `FormatHuman` in `internal/report` and
`internal/doctor`.
