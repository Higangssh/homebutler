# homebutler docs

The source of [docs.homebutler.dev](https://docs.homebutler.dev): a [Docusaurus](https://docusaurus.io) site with the guides, the CLI and MCP references, and an HTTP API reference built from `openapi/homebutler.yaml`.

## Run it locally

Needs Node.js 20 or later.

```bash
cd docs
npm ci
npm start
```

Open http://localhost:3000. If homebutler's own dashboard already uses that port, run `npm start -- --port 3001`.

## Build

```bash
npm run build
```

The site is written to `build/`. The build fails on any broken internal link, so a green build means no broken links. `npm run serve` serves the built site.

## Where things are

| Path | What it holds |
| --- | --- |
| `docs/` | The pages, one `.mdx` file each |
| `sidebars.ts` | The tabs' sidebars and page order |
| `openapi/homebutler.yaml` | The HTTP API spec. The API reference pages are generated from it at build time. |
| `site.config.ts` | Name, address, repo, colours and logo |
| `static/` | Images and files served as they are |

The site loads nothing from other services: search runs in the browser from an index built with the site, fonts are the system's, and there are no analytics.

Editing, components and hosting are covered in [MAINTAINING.md](MAINTAINING.md).
