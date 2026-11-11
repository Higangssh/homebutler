/**
 * Site settings: name, address, repo, colours, logo, tabs and links.
 */
export type SiteConfig = {
  productName: string;
  tagline: string;
  /** Where the site is served, e.g. https://docs.example.com */
  url: string;
  /** '/' when the site has its own domain, '/<path>/' when it is served under a path */
  baseUrl: string;
  /** GitHub org/user and repo, used for "Edit this page" links */
  githubOrg: string;
  githubRepo: string;
  /** Folder of the docs site inside the repo, for "Suggest edits" links ('' if the repo IS the site) */
  docsPathInRepo: string;
  /** Path to the OpenAPI spec, relative to this repo. Empty string disables the API reference. */
  openapiSpec: string;
  /** Owner shown in the preview banner, e.g. "Acme" */
  ownerName: string;
  /** Wordmark next to the icon. Parts with accent: true use the brand colour. */
  brand?: {
    wordmark: { text: string; accent?: boolean }[];
    /** Label after the wordmark, e.g. 'Docs'. Omit for none. */
    suffix?: string;
    /** Font for the wordmark (bundle it in src/fonts.js); defaults to the body font. */
    font?: string;
  };
  theme: {
    primary: string;        // brand colour, light mode
    primaryDark: string;    // brand colour, dark mode
    logoLight: string;      // path under static/
    logoDark: string;
    favicon: string;
  };
  /** Text links in the middle of the page footer (registry, support, status, product site) */
  footerLinks: { label: string; href: string }[];
  /** The product's character or mascot (path under static/), shown under "On this page" on pages with `character: true` in their front matter. Omit for none. */
  character?: { src: string; alt: string };
  /** Links pinned to the bottom of the desktop sidebar (keep it to one or two) */
  sidebarLinks?: { label: string; href: string; icon?: 'github' | 'x' | 'discord' | 'linkedin' }[];
  /** Icon links on the right of the page footer */
  footerSocials?: { icon: 'github' | 'x' | 'discord' | 'linkedin'; href: string; label: string }[];
  /** Top tabs, in order. Each opens its own sidebar from sidebars.ts. The 'api' tab is skipped when openapiSpec is empty. */
  tabs: { label: string; sidebarId: string }[];
  /** Extra navbar links after the tabs (external pages, dashboards) */
  navbarLinks: { label: string; to?: string; href?: string }[];
};

const site: SiteConfig = {
  productName: 'homebutler',
  tagline: 'Know what changed on your server since last time.',
  url: 'https://docs.homebutler.dev',
  baseUrl: '/',
  githubOrg: 'Higangssh',
  githubRepo: 'homebutler',
  docsPathInRepo: 'docs',
  openapiSpec: 'openapi/homebutler.yaml',
  ownerName: 'the homebutler maintainers',
  brand: {
    wordmark: [{ text: 'Homebutler' }],
    suffix: 'Docs',
  },
  theme: {
    primary: '#059669',
    primaryDark: '#10b981',
    logoLight: 'img/mascot-logo.png',
    logoDark: 'img/mascot-logo.png',
    favicon: 'img/favicon.png',
  },
  footerLinks: [
    { label: 'Website', href: 'https://homebutler.dev' },
    { label: 'Report an issue', href: 'https://github.com/Higangssh/homebutler/issues' },
  ],
  character: { src: 'img/mascot.png', alt: 'The homebutler mascot, a butler holding a small server on a tray' },
  sidebarLinks: [{ label: 'GitHub', href: 'https://github.com/Higangssh/homebutler', icon: 'github' }],
  footerSocials: [{ icon: 'github', href: 'https://github.com/Higangssh/homebutler', label: 'GitHub' }],
  tabs: [
    { label: 'Docs', sidebarId: 'docs' },
    { label: 'CLI reference', sidebarId: 'cli' },
    { label: 'MCP tools', sidebarId: 'mcp' },
    { label: 'HTTP API', sidebarId: 'api' },
    { label: 'Changelog', sidebarId: 'changelog' },
  ],
  navbarLinks: [],
};

export default site;
