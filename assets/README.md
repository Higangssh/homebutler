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

## social-preview.html / social-preview.png

The card GitHub serves as `og:image`, so it is what Reddit, X, Discord, and
Slack render when someone pastes a link to the repository. It is not referenced
from the README — GitHub only picks it up once it is uploaded under
**Settings → General → Social preview**, so re-rendering the PNG is not enough
on its own.

It is HTML rather than SVG because this one is rasterised to PNG before it is
ever used, which lifts the sanitiser constraints that shape the terminal cards
above.

Two constraints to know before editing it:

- **1280x640, under 1MB.** GitHub crops anything else, and the platforms that
  read `og:image` letterbox it.
- **It is read as a thumbnail.** The headline is sized for a card scrolling past
  in a feed, not for a full-width browser window, so keep it under about six
  words.

Re-render it after editing:

```bash
python -m http.server 8901 --bind 127.0.0.1

# in another shell
npx playwright screenshot --viewport-size=1280,640 \
  http://127.0.0.1:8901/assets/social-preview.html assets/social-preview.png
```

Then re-upload it under Settings → General → Social preview.
