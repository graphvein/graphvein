// Renders the social preview image (1200 x 630) from the current lead and
// the hero screenshot, and records the lead it drew in content/media.json
// (the site build fails when the lead changes and this image does not).
//
// Run from site/ with Node and Playwright (Chromium) installed:
//   node tools/social.cjs
// Not part of the site build: the PNG is committed.
'use strict';
const fs = require('fs');
const path = require('path');
const { chromium } = require('playwright');

const SITE = path.resolve(__dirname, '..');
const home = JSON.parse(fs.readFileSync(path.join(SITE, 'content/home.json'), 'utf8'));
const mediaPath = path.join(SITE, 'content/media.json');
const media = JSON.parse(fs.readFileSync(mediaPath, 'utf8'));
const entry = media.files.find((f) => f.file === media.social);
if (!entry) throw new Error('media.json has no entry for the social image');
const hero = path.join(SITE, 'media', entry.derived_from);
const logo = path.join(SITE, 'theme/static/logo.svg');

const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
const dataURL = (file, type) => `data:${type};base64,${fs.readFileSync(file).toString('base64')}`;

const page = `<!doctype html><html><head><meta charset="utf-8"><style>
*{box-sizing:border-box;margin:0}html,body{width:1200px;height:630px;overflow:hidden}
body{font-family:system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;
background:linear-gradient(135deg,#17123a,#2b1d5e 55%,#4a2a8a);color:#fff;display:flex}
.l{flex:0 0 610px;padding:60px 0 60px 64px;display:flex;flex-direction:column;justify-content:space-between}
.b{display:flex;align-items:center;gap:16px;font-weight:700;font-size:38px}
.q{font-size:44px;line-height:1.14;font-weight:750;letter-spacing:-.02em}
.m{color:#d0cbf0;font-size:23px}
.r{flex:1;position:relative;overflow:hidden}
.r img{position:absolute;left:36px;top:70px;width:1000px;border-radius:12px 0 0 12px;box-shadow:0 20px 60px rgba(0,0,0,.5)}
</style></head><body>
<div class="l"><div class="b"><img src="${dataURL(logo, 'image/svg+xml')}" width="60" height="60" alt=""><span>Graphvein</span></div>
<p class="q">${esc(home.lead)}</p><p class="m">Local · no LLM · every link says how it was found</p></div>
<div class="r"><img src="${dataURL(hero, 'image/png')}" alt=""></div>
</body></html>`;

(async () => {
  const browser = await chromium.launch();
  const p = await browser.newPage({ viewport: { width: 1200, height: 630 }, deviceScaleFactor: 1 });
  await p.setContent(page, { waitUntil: 'load' });
  await p.screenshot({ path: path.join(SITE, 'media', media.social) });
  await browser.close();
  entry.text = home.lead;
  fs.writeFileSync(mediaPath, JSON.stringify(media, null, 2) + '\n');
  console.log('wrote media/' + media.social);
})().catch((e) => { console.error(e); process.exit(1); });
