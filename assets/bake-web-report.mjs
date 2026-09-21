// Bake assets/web-report.png — the Report tab at desktop width, which is also
// the product shot on homebutler.dev.
//
// It refuses to write the file when what the page renders disagrees with what
// the API answered. That check exists because reading the picture is the only
// thing that has ever caught these, and reading it is exactly what gets
// skipped: the last two defects here were a disk change of 1.6 GB to 1.7 TB,
// and a doctor card badged WARN above a failing finding while claiming nine
// passes it did not list. Both were published. Both were the same shape — a
// value that is the right type and the wrong number, in a file nothing reads.
//
//   ./homebutler serve --demo --host 127.0.0.1 --port 8791
//   cd web && node ../assets/bake-web-report.mjs ../assets/web-report.png
//
// Build with a real tag (`make build VERSION=v0.37.0`). `git describe` on a
// branch produces something like v0.37.0-5-gf3549a7, and a version nobody can
// install does not belong in a picture we publish.
// Playwright belongs to web/, and an import in this file would be resolved
// against assets/ instead. Run this from web/.
import { createRequire } from 'node:module';
const { chromium } = createRequire(process.cwd() + '/')('playwright');

const BASE = 'http://127.0.0.1:8791';
const out = process.argv[2] ?? 'web-report.png';

const [report, doctor, status] = await Promise.all(
  ['/api/report', '/api/doctor', '/api/status'].map((p) =>
    fetch(BASE + p).then((r) => r.json()),
  ),
);

// A size in gigabytes, from "1.6 TB" or "800 GB".
const gb = (s) => {
  const m = /^([\d.]+)\s*(GB|TB)$/.exec(s.trim());
  return m ? Number(m[1]) * (m[2] === 'TB' ? 1000 : 1) : null;
};

const browser = await chromium.launch();
const page = await browser.newPage({
  viewport: { width: 1200, height: 900 },
  deviceScaleFactor: 2,
  colorScheme: 'dark',
});
await page.goto(BASE + '/', { waitUntil: 'networkidle' });
await page.getByRole('button', { name: 'Report' }).click();
await page.waitForTimeout(1500);

const rendered = await page.evaluate(() => document.querySelector('main').innerText);

// What the page says, against what the machine said. Counts rather than
// wording: the sentences are written for a person and are allowed to improve.
const severities = (doctor.findings ?? []).reduce((acc, f) => {
  acc[f.severity] = (acc[f.severity] ?? 0) + 1;
  return acc;
}, {});
const problems = [];

const badge = rendered.match(/Doctor\n(FAIL|WARN|PASS)\n/)?.[1]?.toLowerCase();
if (badge !== doctor.status) {
  problems.push(`doctor badge reads ${badge}, the API says ${doctor.status}`);
}

const counted = rendered.match(/(\d+) failing · (\d+) to look at · (\d+) fine/);
if (!counted) {
  problems.push('the doctor summary line is not on the page');
} else {
  const [, fail, warn, pass] = counted.map(Number);
  const want = [severities.fail ?? 0, severities.warn ?? 0, severities.pass ?? 0];
  if (String([fail, warn, pass]) !== String(want)) {
    problems.push(`summary reads ${[fail, warn, pass]}, the findings are ${want}`);
  }
}

// The disk rows against the machine's own disks. Checking the page against
// the API would not have caught the defect that made this script exist: the
// report said a mount went from 1.6 GB to 1.7 TB and the API said exactly
// that too, because both are the same demo data. The reading that contradicts
// it is the mount's actual size, on a different endpoint — 1740 of 2000 GB,
// which no 1.6 GB history can lead to. Agreement between two views of one
// wrong number is not a check.
for (const change of (report.notable_changes ?? []).filter((c) => c.kind === 'disk')) {
  const disk = (status.disks ?? []).find((d) => d.mount === change.target);
  if (!disk) {
    problems.push(`disk change names ${change.target}, which the machine does not report`);
    continue;
  }
  const [from, to] = (change.detail ?? '').split('→').map((s) => gb(s) ?? NaN);
  if (Number.isNaN(from) || Number.isNaN(to)) {
    problems.push(`${change.target}: cannot read sizes out of "${change.detail}"`);
    continue;
  }
  // The end of the change is where the mount is now, and a mount does not
  // arrive at 1.7 TB from a tenth of its own size.
  if (Math.abs(to - disk.used_gb) > disk.total_gb * 0.1) {
    problems.push(`${change.target} ends at ${to} GB, the machine has ${disk.used_gb} GB used`);
  }
  if (from < disk.total_gb * 0.1) {
    problems.push(`${change.target} starts at ${from} GB on a ${disk.total_gb} GB mount — check the unit`);
  }
}

for (const change of report.notable_changes ?? []) {
  if (!rendered.includes(change.target)) {
    problems.push(`notable change ${change.kind} ${change.target} is not on the page`);
  }
  if (change.detail && !rendered.includes(change.detail)) {
    problems.push(`${change.target}: the page does not carry "${change.detail}"`);
  }
}

if (problems.length) {
  console.error('the page disagrees with the API, so no image was written:\n');
  for (const p of problems) console.error('  - ' + p);
  await browser.close();
  process.exit(1);
}

// fullPage, or the clip is silently trimmed to the viewport and the last card
// loses its bottom border — a file is still produced, which is how that one
// shipped.
const bottom = await page.evaluate(() => {
  const s = document.querySelectorAll('main .section');
  return s[s.length - 1].getBoundingClientRect().bottom + window.scrollY;
});
await page.screenshot({
  path: out + '.2x.png',
  fullPage: true,
  clip: { x: 0, y: 0, width: 1200, height: Math.ceil(bottom) + 28 },
});
await browser.close();

console.log(`wrote ${out}.2x.png — resample to 1200 wide to match web-dashboard.png`);
console.log(`checked ${(report.notable_changes ?? []).length} changes and the doctor counts against the API`);
