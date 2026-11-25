// JSK-T217 capture: real jobseekerd, real Chromium, virtual passkey. For every language in LANGS and both colour schemes it
// screenshots the provider-expansion pages (settings with providers/apply policy/employer policies, manual intake, merge queue,
// Position page) and records <html lang/dir> read from the live DOM. Output: <EVIDENCE_DIR>/frames/<page>-<lang>-<scheme>.png + manifest.json.
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
const E = process.env, base = E.BASE_URL, ev = path.join(E.EVIDENCE_DIR, 'frames');
const langs = (E.LANGS || 'en').split(',');
fs.mkdirSync(ev, { recursive: true });
(async () => {
  const browser = await chromium.launch({ args: ['--host-resolver-rules=MAP js.lan 127.0.0.1', '--ignore-certificate-errors'] });
  const manifest = [];
  // ONE signed-in context (the bootstrap token is single use); Accept-Language is switched per language, so the console negotiates it
  // (no stored choice), and the colour scheme per frame
  const ctx = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 900 } });
  const page = await ctx.newPage();
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('WebAuthn.enable');
  await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
  await page.goto(base + '/login', { waitUntil: 'networkidle' });
  await page.fill('#bootstrap-token', E.BOOTSTRAP_TOKEN);
  await page.click('#passkey-register');
  await page.waitForFunction(() => document.getElementById('passkey-status').textContent === 'ok', null, { timeout: 15000 });
  await page.click('#passkey-login');
  await page.waitForURL(base + '/', { timeout: 15000 });
  await page.goto(base + '/postings/2', { waitUntil: 'networkidle' });
  const posPath = new URL(page.url()).pathname;
  const pages = { settings: '/settings', intake: '/intake', merges: '/queue/merges', position: posPath };
  for (const lang of langs) {
    await page.setExtraHTTPHeaders({ 'Accept-Language': lang });
    for (const scheme of ['light', 'dark']) {
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      for (const [name, p] of Object.entries(pages)) {
        await page.goto(base + p, { waitUntil: 'networkidle' });
        const meta = await page.evaluate(() => ({ lang: document.documentElement.lang, dir: document.documentElement.dir, title: document.title, bg: getComputedStyle(document.body).backgroundColor }));
        const file = `${name}-${lang}-${scheme}.png`;
        await page.screenshot({ path: path.join(ev, file) });
        manifest.push({ file, page: name, path: p, lang, scheme, ...meta });
      }
    }
  }
  fs.writeFileSync(path.join(E.EVIDENCE_DIR, 'manifest.json'), JSON.stringify(manifest, null, 1));
  await browser.close();
})().catch(e => { console.error(String(e)); process.exit(1); });
