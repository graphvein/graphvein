// Layout check of the generated site (not run in CI): serves the output
// folder on 127.0.0.1, opens the pages at 1440, 900 and 390 px wide in
// light and dark, and fails on horizontal overflow or when the primary
// call to action is not in the first screen at 1440 x 900, or when the
// screenshot lightbox does not open, close on Esc and return focus
// (1440 and 390). It also checks that screenshots show in the theme
// opposite to the page's, that the lightbox opens the variant that was
// clicked, and that the header theme switch (System / Light / Dark) sets
// the theme before the body is drawn, is remembered across a reload and
// returns to the system theme. Screenshots go
// to the folder given as the second argument (keep it out of Git).
//
//   node tools/check-layout.cjs DIST_DIR SHOTS_DIR
'use strict';
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const [dist, shots] = process.argv.slice(2);
if (!dist || !shots) {
  console.error('usage: node tools/check-layout.cjs DIST_DIR SHOTS_DIR');
  process.exit(2);
}
fs.mkdirSync(shots, { recursive: true });
const types = { '.html': 'text/html', '.css': 'text/css', '.js': 'text/javascript', '.svg': 'image/svg+xml', '.png': 'image/png', '.txt': 'text/plain' };

const server = http.createServer((req, res) => {
  let p = decodeURIComponent(new URL(req.url, 'http://x').pathname);
  if (p.endsWith('/')) p += 'index.html';
  const file = path.join(dist, path.normalize(p));
  if (!file.startsWith(path.resolve(dist)) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) {
    res.writeHead(404, { 'content-type': 'text/html' });
    res.end(fs.readFileSync(path.join(dist, '404.html')));
    return;
  }
  res.writeHead(200, { 'content-type': types[path.extname(file)] || 'application/octet-stream' });
  res.end(fs.readFileSync(file));
});

const pages = [['home', '/'], ['docs', '/docs/evidence/link-precision/'], ['adr', '/docs/adr/0091-project-website/'], ['download', '/download/']];

// Records data-theme when <body> first appears, before anything is drawn:
// a stored choice must already be applied then (no flash).
const recordThemeAtBody = () => {
  new MutationObserver((_, o) => {
    if (document.body) {
      window.__themeAtBody = document.documentElement.getAttribute('data-theme');
      o.disconnect();
    }
  }).observe(document, { childList: true, subtree: true });
};

// The theme of the page as drawn: the body's background luminance.
const pageTheme = () => {
  const m = getComputedStyle(document.body).backgroundColor.match(/\d+/g).map(Number);
  return (0.299 * m[0] + 0.587 * m[1] + 0.114 * m[2]) > 128 ? 'light' : 'dark';
};

// The theme variant of the visible hero screenshot.
const heroVariant = () => {
  const imgs = [...document.querySelectorAll('.hero img')].filter((i) => i.offsetParent !== null);
  if (imgs.length !== 1) return 'visible: ' + imgs.length;
  return imgs[0].getAttribute('src').includes('-dark') ? 'dark' : 'light';
};
const widths = [[1440, 900], [900, 900], [390, 844]];

server.listen(0, '127.0.0.1', async () => {
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch();
  const failures = [];
  for (const scheme of ['light', 'dark']) {
    for (const [w, h] of widths) {
      const ctx = await browser.newContext({ viewport: { width: w, height: h }, deviceScaleFactor: 2, colorScheme: scheme });
      await ctx.addInitScript(recordThemeAtBody);
      const page = await ctx.newPage();
      for (const [name, url] of pages) {
        await page.goto(base + url, { waitUntil: 'load' });
        const m = await page.evaluate(() => {
          const el = document.scrollingElement;
          const cta = document.getElementById('cta-primary');
          const r = cta && cta.getBoundingClientRect();
          return { sw: el.scrollWidth, iw: window.innerWidth, cta: r ? { top: r.top, bottom: r.bottom } : null };
        });
        const tag = `${name}-${w}-${scheme}`;
        const status = [];
        if (m.sw > m.iw) {
          failures.push(`${tag}: horizontal overflow (${m.sw} > ${m.iw})`);
          status.push('OVERFLOW');
        }
        if (name === 'home' && w === 1440) {
          if (!m.cta || m.cta.bottom > h) failures.push(`${tag}: primary call to action below the fold`);
          status.push(`cta bottom ${m.cta ? Math.round(m.cta.bottom) : 'none'} of ${h}`);
        }
        if (name === 'home') {
          const t = await page.evaluate(pageTheme);
          const v = await page.evaluate(heroVariant);
          const want = scheme === 'light' ? 'dark' : 'light';
          if (t !== scheme) failures.push(`${tag}: page drawn ${t}, system ${scheme}`);
          if (v !== want) failures.push(`${tag}: hero screenshot ${v}, want ${want} on a ${scheme} page`);
          status.push(`page ${t}, hero ${v}`);
        }
        if (name === 'home' && (w === 1440 || w === 390)) {
          // The lightbox: opens on a feature screenshot, in the variant
          // that was clicked; Esc closes it and focus returns to the link.
          const zoom = page.locator('a.zoom:visible').first();
          await zoom.scrollIntoViewIfNeeded();
          const href = await zoom.getAttribute('href');
          await zoom.click();
          await page.waitForSelector('dialog.lightbox[open]');
          await page.waitForFunction(() => { const i = document.querySelector('dialog.lightbox img'); return i && i.complete && i.naturalWidth > 0; });
          const shown = await page.evaluate(() => document.querySelector('dialog.lightbox img').getAttribute('src'));
          const want = scheme === 'light' ? '-dark' : '-light';
          if (shown !== href || !shown.includes(want)) failures.push(`${tag}: lightbox shows ${shown} for the thumbnail ${href}`);
          status.push(`lightbox ${path.basename(shown)}`);
          await page.screenshot({ path: path.join(shots, `${tag}-lightbox.png`) });
          await page.keyboard.press('Escape');
          const lb = await page.evaluate(() => ({
            open: !!document.querySelector('dialog.lightbox[open]'),
            focus: document.activeElement && document.activeElement.classList.contains('zoom'),
          }));
          if (lb.open) failures.push(`${tag}: Esc did not close the lightbox`);
          if (!lb.focus) failures.push(`${tag}: focus did not return to the screenshot link`);
          status.push(`lightbox open/Esc ${lb.open ? 'FAILED' : 'ok'}, focus ${lb.focus ? 'returned' : 'LOST'}`);
          if (w === 1440) {
            const what = page.locator('section[aria-labelledby="h-what"]');
            await what.screenshot({ path: path.join(shots, `${tag}-what-it-does.png`) });
          }
          await page.evaluate(() => window.scrollTo(0, 0));
        }
        await page.screenshot({ path: path.join(shots, `${tag}-fold.png`) });
        await page.screenshot({ path: path.join(shots, `${tag}-full.png`), fullPage: true });
        console.log(`${tag}: scrollWidth ${m.sw} / ${m.iw}${status.length ? '; ' + status.join('; ') : ''}`);
      }
      if (w === 1440 || w === 390) {
        // The theme switch: pick the theme opposite to the system's, check
        // the page and the screenshots follow it, that it is applied before
        // the body after a reload, then go back to System.
        const tag = `switch-${w}-${scheme}`;
        const other = scheme === 'light' ? 'dark' : 'light';
        await page.goto(base + '/', { waitUntil: 'load' });
        const sw = page.locator('.theme');
        if (!(await sw.isVisible())) failures.push(`${tag}: the theme switch is not visible`);
        await page.click(`[data-theme-choice="${other}"]`);
        let r = await page.evaluate(() => ({ attr: document.documentElement.getAttribute('data-theme') }));
        r.page = await page.evaluate(pageTheme);
        r.hero = await page.evaluate(heroVariant);
        if (r.attr !== other || r.page !== other || r.hero !== scheme) failures.push(`${tag}: after ${other}: ${JSON.stringify(r)}`);
        await page.screenshot({ path: path.join(shots, `${tag}-${other}.png`) });
        await page.reload({ waitUntil: 'load' });
        const atBody = await page.evaluate(() => window.__themeAtBody);
        const pressed = await page.getAttribute(`[data-theme-choice="${other}"]`, 'aria-pressed');
        if (atBody !== other) failures.push(`${tag}: after a reload data-theme was ${atBody} when the body appeared`);
        if (pressed !== 'true') failures.push(`${tag}: the ${other} button is not pressed after a reload`);
        await page.click('[data-theme-choice="system"]');
        const back = await page.evaluate(() => ({ attr: document.documentElement.getAttribute('data-theme'), stored: localStorage.getItem('graphvein-theme') }));
        const backPage = await page.evaluate(pageTheme);
        if (back.attr !== null || back.stored !== null || backPage !== scheme) failures.push(`${tag}: System did not restore the system theme: ${JSON.stringify(back)} ${backPage}`);
        console.log(`${tag}: ${other} ok=${r.attr === other}, reload at body ${atBody}, back to system ${backPage}`);
      }
      await ctx.close();
    }
  }
  await browser.close();
  server.close();
  if (failures.length) {
    console.error('FAIL\n' + failures.join('\n'));
    process.exit(1);
  }
  console.log('layout check passed');
});
