# Maintaining these docs

The docs are plain Markdown files. Edit a file, open a pull request, merge: the host rebuilds and publishes the site from `main`. See [Hosting](#hosting).

## Common tasks

| Task | How |
|---|---|
| Edit a page | Edit the `.mdx` file in `docs/`, or click "Suggest edits" at the bottom of the page on the live site. |
| Add a page | Add a `.mdx` file in `docs/` (or `docs/guides/`). Give it a `title` and `description` in the frontmatter. Add it to `sidebars.ts` if it isn't in an auto-generated section. |
| Update the API reference | Edit the spec in `openapi/`. `npm start` and `npm run build` regenerate the pages from it; after removing an endpoint, run `npm run clean-api` first. Never hand-edit files in `docs/reference/api/`; they're regenerated. |
| Preview locally | `npm ci`, then `npm start` and open http://localhost:3000 followed by `baseUrl` from `site.config.ts`. If the product itself runs on port 3000, use `npm start -- --port 3001`. |
| Check before merging | `npm run build` fails on broken internal links, so a green build means no broken links. |

## Components you can use in any page (no import needed)

`<Card>`, `<CardGroup cols={2}>`, `<Steps>` + `<Step title="">`, `<Note>`, `<Tip>`, `<Warning>`, `<Info>`, `<Check>`, `<Accordion>`, `<AccordionGroup>`, `<Badge>`, `<Endpoint method="GET" path="/x">`, `<ParamField name="" type="" default="">`, `<Frame caption="">`, `<Tabs>` + `<TabItem>`, `<Tool>`.

Card icons: rocket, book, code, terminal, plug, gear, bolt, shield, search, info, lightbulb, warning, check.

## Reference entries with `<Tool>`

Wrap each MCP tool, command or operation in `<Tool>`. The name heading shows in "On this page"; the labels below it are styled as small section labels; the examples stay collapsed until opened:

````mdx
<Tool>

## create_item

<Badge tone="warning">write</Badge>

Creates one item.

### Parameters

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | Yes | none | Item name. |

### Response

An object with `id` and `name`.

<Accordion title="Examples">

Arguments:

```json
{ "name": "demo" }
```

</Accordion>

</Tool>
````

With `##` tool names, add `toc_max_heading_level: 2` to the page's front matter. With `###` tool names under `##` groups, use `####` for the labels.

## Settings

Product name, URLs, colours and logo live in `site.config.ts`. Colours for light and dark mode are the first lines of `src/css/theme.css`.

The site uses system fonts, so it loads none. If you add a font, bundle it from an `@fontsource` package instead of loading it from a third-party service: every visitor's browser would contact it.

## Hosting

The site is static files. Any host that builds from the repo works, such as Cloudflare Pages, Netlify or Vercel, with these settings:

| Setting | Value |
| --- | --- |
| Root directory | `docs` |
| Build command | `npm ci && npm run build` |
| Output directory | `build` |
| Node.js | 20 or later |

No environment variables, secrets or workflows are needed. Point `docs.homebutler.dev` at the host with the DNS record it asks for. If the address changes, update `url` and `baseUrl` in `site.config.ts`, because `llms.txt`, the sitemap and the `.md` copies use it.

A normal build is the official site: indexed, with a sitemap and "Suggest edits" links. `DOCS_MODE=preview` builds a copy for another address instead, with an "unofficial preview" banner, `noindex` and a `robots.txt` that blocks crawlers.

## AI-readable output

Every build publishes `/llms.txt` (every sidebar page with its description, grouped under the sidebar headings), a `.md` copy of each page (API reference pages carry their slice of the spec as YAML), and the OpenAPI spec at `/openapi.yaml`. Nothing to maintain: a page added to a sidebar shows up in both, and its `description` frontmatter becomes the llms.txt summary.
