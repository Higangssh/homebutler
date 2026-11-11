// Right-hand column: "On this page", plus the client's character (site.config `character`)
// under it on pages whose front matter sets `character: true`.
import React from 'react';
import TOCDesktop from '@theme-original/DocItem/TOC/Desktop';
import { useDoc } from '@docusaurus/plugin-content-docs/client';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import useBaseUrl from '@docusaurus/useBaseUrl';
import type { SiteConfig } from '@site/site.config';

export default function DocItemTOCDesktopWrapper() {
  const { frontMatter } = useDoc();
  const { siteConfig } = useDocusaurusContext();
  const character = (siteConfig.customFields?.site as SiteConfig).character;
  const src = useBaseUrl(character?.src ?? '');
  const show = Boolean(character && (frontMatter as { character?: boolean }).character);
  return (
    <div className="ds-toc-column">
      <TOCDesktop />
      {show && (
        <div className="ds-toc-character">
          <img src={src} alt={character!.alt} loading="lazy" />
        </div>
      )}
    </div>
  );
}
