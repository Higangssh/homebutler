// Preview mode: block indexing so an unofficial preview never competes with the
// owner's own pages. Docusaurus `noIndex` adds the meta tag; this adds robots.txt.
const fs = require('node:fs');
const path = require('node:path');

module.exports = function previewMode(_context, { isPreview }) {
  return {
    name: 'preview-mode',
    async postBuild({ outDir }) {
      const robots = isPreview ? 'User-agent: *\nDisallow: /\n' : 'User-agent: *\nAllow: /\n';
      fs.writeFileSync(path.join(outDir, 'robots.txt'), robots);
    },
  };
};
