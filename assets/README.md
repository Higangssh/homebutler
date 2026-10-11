# assets

Images referenced by the README and by homebutler.dev. Nothing here ships in
the binary.

Where an image is used in both places, **this directory holds the original** and
the site copies it. The landing page repository runs `tools/check-assets.py`,
which fails when a file that exists in both is not byte-identical — one copy
getting fixed and the other not is the failure that keeps happening here.

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
`internal/doctor`. `TestRenderedChangeBlock` pins every section the card shows —
the attention line, the change block, the header's comparison stamp and the
suggested action — so a format change fails a test rather than quietly dating
the image. It pinned only the change block once, and the card spent a release
showing a sentence the binary no longer printed.

## report-demo.gif / report-demo.tape

The recording at the top of the README. **It is `homebutler report` running,
not a picture of it.** `report-demo.tape` is a [VHS](https://github.com/charmbracelet/vhs)
tape that types the command into an 80-column terminal, and the command is
the 1.1.1 release binary running on a Raspberry Pi with two real containers.

Recorded on 2026-10-11, in UTC as the frame shows it:

1. In a home directory of its own on the Pi, `homebutler install filebrowser`
   and `homebutler install uptime-kuma`. The first `report` ran after both had
   been up for over a minute, from a short ssh session. A process younger than
   a minute is left out of a snapshot and would turn up later as `new` (#315).
2. Two hours later, filebrowser was recreated with `docker compose up -d
   --force-recreate`, and uptime-kuma's image changed from `:1` to `:2`.
3. The tape ran `report --no-save`, so it can be recorded again without moving
   the baseline.

Two things sit between the binary and the frame, and both are in a wrapper
that is not committed, because it names the host:

- **The host name is replaced with `homelab`.** The Pi's name is printed in the
  header and the Host line, homebutler has no setting for it, and the Pi does
  not allow the unprivileged namespace that would rename it. `sed` swaps that
  one word in the output stream. No other byte is changed.
- **The terminal's reply to a colour query is discarded.** The styling library
  asks the terminal for its background colour. Through ssh, the answer arrives
  after the remote program has exited, and it used to land on the next prompt
  as `11;rgb:1e1e/…`. That is not homebutler output, and a terminal running
  homebutler directly never shows it.

**Do not edit the output to make a cleaner picture.** Three recordings were
thrown away, not touched up. The first had `new systemd` and `(sd-pam)`,
which came from the baseline being taken too soon. The second, on 1.0.0, had
five process lines repeating the two container changes, and that one was fixed
in the product (1.1.0, #325) rather than in the image. The third, on 1.1.0, was
clean but showed `CPU 0.0%` on a machine averaging 0.65%, because CPU was
sampled over 200 ms. That was fixed in 1.1.1 (#333). If a recording shows a line
that should not be there, or a number that is not true, the image is not where
it gets fixed. Nor is picking the take where the number looks nicest: an idle
second really can read 0.0%.

To record it again, recreate the setup above, write a wrapper that defines
`homebutler` as the ssh call (with the two substitutions above) and sets
`PS1='$ '`, then run:

```bash
HB_DEMO_ENV=/path/to/wrapper.sh vhs assets/report-demo.tape
```

It is 760x370 (80 columns at size 15), about 72 KB, and ten seconds long.
At 80 columns the uptime-kuma line wraps, as it would in a real terminal of that
width. That is what keeps it legible on a phone: rendered in Chromium with a
Pixel 7 profile, at 412 CSS pixels wide, the text is small but readable. The
1240-wide first take was not.

## demo.gif

The run-through at the top of the README, and the same frames as the video it
links to. 820px wide, 1.6MB — the README's ceiling is 2MB, and a phone loads
this before it loads anything else on the page.

**The text in it is captured output, not typed.** The generator reads
`captures/*.txt` — the real `report`, `report --json`, `doctor` and
`backup drill` from a demo host — and only colours and lays them out; no scene
carries a text literal. That order matters: an earlier version was transcribed
by hand and showed `report --json` objects without their `text` field, which is
a field the command always emits. A picture of output the binary does not
produce is the same mistake as a sentence about behaviour it does not have, and
harder to notice.

So when the report format changes, re-capture and re-render. Do not edit the
image.

The video it links to is the same frames with crossfades and **no audio track**,
so do not describe it as something to listen to. The link is there because it
plays larger than a GIF in a README column.

`install-demo.gif` and `demo-thumbnail.png` were deleted in #219 and are still
reachable through the raw URL of the commit that held them, which is how git
works. That was left alone deliberately: what the install recording exposed was
a macOS home path containing the maintainer's name, and the same name is the
author of every commit in the repository, so rewriting public history buys
nothing. An image carrying a token or a key would be a different decision, and
the first move there is rotating the credential rather than rewriting history.

Two things to fix at the next re-capture, left as they are because they are
real output rather than invented:

- The demo home is `/tmp/homelab`, so the backup paths read
  `/tmp/homelab/.homebutler/backups/…`. `/tmp` is where the operating system
  throws things away, which is the wrong impression for the one feature about
  keeping them. Capture with the demo home at `/home/demo`.
- The exposed ports are `18091-18093`, which read as a test rig. Familiar
  numbers — 8080, 8096, 3000 — read as somebody's actual machine.

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

## web-report.png

The Report tab at desktop width, used as the product shot on homebutler.dev. It
lived only in the site repository until 2026-09-21, which made it the one asset
with nothing to compare against.

Re-bake it after any change to the Report tab or to the demo data behind it:

```bash
make build-web
make build VERSION=v0.37.0     # a real tag: the footer carries the version and
                               # a dev build string names something nobody can
                               # install
./homebutler serve --demo --host 127.0.0.1 --port 8791

cd web && node ../assets/bake-web-report.mjs ../assets/web-report
# then resample the .2x.png down to 1200 wide
```

`bake-web-report.mjs` **refuses to write the file when the numbers do not hold
up.** Two defects shipped in this picture on the day it was written, and
neither was a broken layout:

- a mount reported going from `1.6 GB` to `1.7 TB`, a thousandfold, while the
  same demo described it as 1740 of 2000 GB
- a `doctor` card badged `WARN` above a failing finding, claiming nine passes
  while listing one — a state `overallStatus` cannot return

Both are the right type in the right place and the wrong number, which is what
gets published: a layout breaks visibly, a number does not. So the script reads
the rendered page and checks it against `/api/report`, `/api/doctor` and
`/api/status` — the doctor badge against the status, the summary against the
findings it lists, and each disk change against the mount's actual size.

That last one is the point. Checking the page against `/api/report` alone would
not have caught the disk defect, because the API said `1.6 GB` too — both are
the same demo data. What contradicts it is a different endpoint: no mount
arrives at 1.7 TB from a tenth of its own size. **Two views agreeing about one
wrong number is not a check.** Both defects were re-introduced and the script
was confirmed to fail on each before this was written.

Take it from `--demo`, never from a real machine. The demo profile is the only
thing that keeps a hostname, a private address, or a home directory path out of
a picture, and a picture is the one place scrubbing cannot reach: the text in it
is pixels.
