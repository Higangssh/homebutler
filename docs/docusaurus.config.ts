import type { Config } from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';
import { themes as prismThemes } from 'prism-react-renderer';
import site from './site.config';
import { prismVarTheme } from './src/lib/prismVarTheme';

const codeTheme = prismVarTheme(prismThemes.github, prismThemes.oneDark);

// Site-specific values come from site.config.ts.
// A normal build is the official site. DOCS_MODE=preview adds an unofficial-preview
// banner, noindex and a blocking robots.txt, for a copy hosted anywhere else.
const isPreview = process.env.DOCS_MODE === 'preview';
const hasApi = Boolean(site.openapiSpec);
// Where the site is served: site.config.ts, unless SITE_URL / SITE_BASE_URL override it
// for a build hosted at another address.
const siteUrl = process.env.SITE_URL || site.url;
const baseUrl = process.env.SITE_BASE_URL || site.baseUrl;
const editBase = `https://github.com/${site.githubOrg}/${site.githubRepo}/edit/main/${site.docsPathInRepo ? site.docsPathInRepo + '/' : ''}`;

const config: Config = {
  title: site.productName,
  tagline: site.tagline,
  favicon: site.theme.favicon,
  url: siteUrl,
  baseUrl,
  organizationName: site.githubOrg,
  projectName: site.githubRepo,
  trailingSlash: false,
  onBrokenLinks: 'throw',
  markdown: { mermaid: false, hooks: { onBrokenMarkdownLinks: 'throw' } },
  noIndex: isPreview,
  customFields: { isPreview, site },
  headTags: [
    { tagName: 'style', attributes: { id: 'ds-code-colours' }, innerHTML: codeTheme.css },
  ],

  presets: [
    [
      'classic',
      {
        docs: {
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          // A preview copy has no edit links.
          editUrl: isPreview ? undefined : editBase,
          showLastUpdateTime: false,
        },
        blog: false,
        theme: { customCss: ['./src/css/theme.css', './src/css/brand.css'] },
        sitemap: isPreview ? false : { changefreq: 'weekly', priority: 0.5 },
      } satisfies Preset.Options,
    ],
  ],

  plugins: [
    './src/plugins/search-index.js',
    ['./src/plugins/preview-mode.js', { isPreview }],
    // llms.txt sections follow the top tabs: their order, and their labels as headings.
    ['./src/plugins/ai-readable.js', {
      openapiSpec: site.openapiSpec, productName: site.productName, tagline: site.tagline,
      sidebarOrder: site.tabs.map((t) => t.sidebarId),
      sidebarTitles: Object.fromEntries(site.tabs.map((t) => [t.sidebarId, t.label])),
      specSidebar: 'api',
    }],
  ],


  themeConfig: {
    colorMode: { respectPrefersColorScheme: true },
    announcementBar: isPreview
      ? {
          id: 'preview-banner',
          content: `Unofficial documentation preview for ${site.productName}${site.builtBy ? `, built by ${site.builtByUrl ? `<a href="${site.builtByUrl}">${site.builtBy}</a>` : site.builtBy}` : ""}. The project is on <a href="https://github.com/${site.githubOrg}/${site.githubRepo}">GitHub</a>.`,
          isCloseable: false,
        }
      : undefined,
    navbar: {
      title: site.productName,
      logo: { alt: site.productName, src: site.theme.logoLight, srcDark: site.theme.logoDark },
      hideOnScroll: false,
      items: [
        ...site.tabs
          .filter((t) => hasApi || t.sidebarId !== 'api')
          .map((t) => ({ type: 'docSidebar', sidebarId: t.sidebarId, label: t.label, position: 'left' as const })),
        ...site.navbarLinks.map((l) => ({ ...l, position: 'left' as const })),
        { type: 'custom-search', position: 'right' },
      ],
    },
    docs: { sidebar: { hideable: false, autoCollapseCategories: false } },
    prism: {
      // One CSS-variable theme for both modes, so code never flashes the wrong colours on load
      theme: codeTheme.theme,
      darkTheme: codeTheme.theme,
      additionalLanguages: ['bash', 'json', 'yaml', 'docker', 'python', 'typescript'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
