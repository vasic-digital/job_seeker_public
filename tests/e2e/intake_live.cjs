// JSK-T200 console paste form live walk: real jobseekerd, real Chromium, virtual passkey. See intake_live_test.go for the evidence class.
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
const E = process.env, base = E.BASE_URL, ev = E.EVIDENCE_DIR;
const axeSrc = fs.readFileSync(E.AXE_JS, 'utf8');
let failed = 0;
function step(name, ok, detail) { console.log((ok ? 'PASS ' : 'FAIL ') + name + (detail ? ' :: ' + detail : '')); if (!ok) failed++; }
async function axe(page, name, scheme) {
  await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
  await page.evaluate(axeSrc);
  const r = await page.evaluate(async () => await axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'] } }));
  const v = r.violations.map(x => x.id + '(' + x.nodes.length + '): ' + x.nodes.slice(0, 2).map(n => n.target.join(' ')).join(' | '));
  step('axe ' + name + ' ' + scheme, v.length === 0, v.join('; ') || (r.passes.length + ' rules passed'));
}
(async () => {
  const browser = await chromium.launch({ args: ['--host-resolver-rules=MAP js.lan 127.0.0.1', '--ignore-certificate-errors'] });
  const ctx = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 900 } });
  const page = await ctx.newPage();
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('WebAuthn.enable');
  await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
  const errors = []; page.on('pageerror', e => errors.push(String(e))); page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
  await page.goto(base + '/');
  await page.fill('#bootstrap-token', E.BOOTSTRAP_TOKEN);
  await page.click('#passkey-register');
  await page.waitForFunction(() => document.getElementById('passkey-status').textContent === 'ok', null, { timeout: 15000 }).catch(() => {});
  await page.click('#passkey-login');
  await page.waitForURL(base + '/', { timeout: 15000 }).catch(() => {});
  step('signed in', new URL(page.url()).pathname === '/');

  const t = async sel => (await page.locator(sel).first().innerText()).trim();
  await page.click('nav a[href="/intake"]');
  step('nav link opens /intake', new URL(page.url()).pathname === '/intake', page.url());
  for (const id of ['in-url', 'in-title', 'in-company', 'in-text']) step('label bound to #' + id, (await page.locator('label[for=' + id + ']').count()) === 1);
  step('no missing i18n key', !(await page.content()).includes('!!'));
  await page.screenshot({ path: path.join(ev, 'intake-desktop-light.png'), fullPage: true });
  await axe(page, 'intake', 'light'); await axe(page, 'intake', 'dark');
  await page.screenshot({ path: path.join(ev, 'intake-desktop-dark.png'), fullPage: true });
  await page.emulateMedia({ colorScheme: 'light' });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: path.join(ev, 'intake-mobile.png'), fullPage: true });
  step('mobile: no horizontal page scroll', await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1));
  await page.setViewportSize({ width: 1280, height: 900 });

  // a refused link: the browser's own url validation is bypassed on purpose (an https-looking link on a LAN suffix passes type=url) so the SERVER decides
  await page.fill('#in-url', 'https://printer.local/jobs/1'); await page.fill('#in-title', 'Refused Role'); await page.fill('#in-text', 'SECRETBODYMARKER');
  const bad = await Promise.all([page.waitForResponse(r => r.request().method() === 'POST' && /\/intake$/.test(r.url())), page.click('[data-testid=intake-submit]')]);
  step('inward host refused with HTTP 400', bad[0].status() === 400, String(bad[0].status()));
  step('error alert shown with the closed code validation', (await page.locator('[data-error-code=validation]').count()) === 1);
  step('pasted text is not echoed back', !(await page.content()).includes('SECRETBODYMARKER'));
  step('link and title are kept for correction', (await page.inputValue('#in-url')) === 'https://printer.local/jobs/1' && (await page.inputValue('#in-title')) === 'Refused Role');
  await page.screenshot({ path: path.join(ev, 'intake-refused.png'), fullPage: true });
  await axe(page, 'intake-refused', 'light'); await axe(page, 'intake-refused', 'dark'); await page.emulateMedia({ colorScheme: 'light' });

  // a valid paste, submitted from the keyboard (Enter in the title field)
  await page.fill('#in-url', 'https://boards.example.com/jobs/777'); await page.fill('#in-title', 'Staff Go Engineer'); await page.fill('#in-company', 'Acme');
  await page.fill('#in-text', 'We are hiring a staff Go engineer to build payment services for remote teams in Europe.');
  await page.locator('#in-title').focus();
  await Promise.all([page.waitForURL(/\/positions\/\d+$/, { timeout: 10000 }), page.keyboard.press('Enter')]);
  const pos = new URL(page.url()).pathname;
  step('valid paste lands on its Position', /^\/positions\/\d+$/.test(pos), page.url());
  step('Position shows the pasted title and the manual host as source', (await t('[data-testid=position-title]')).startsWith('Staff Go Engineer') && (await page.content()).includes('manual:boards.example.com'), await t('[data-testid=position-title]'));
  await page.screenshot({ path: path.join(ev, 'intake-landed.png'), fullPage: true });

  // the same link again is idempotent
  await page.goto(base + '/intake');
  await page.fill('#in-url', 'https://boards.example.com/jobs/777'); await page.fill('#in-title', 'Staff Go Engineer'); await page.fill('#in-company', 'Acme');
  await page.fill('#in-text', 'We are hiring a staff Go engineer to build payment services for remote teams in Europe.');
  await Promise.all([page.waitForURL(/\/positions\/\d+$/, { timeout: 10000 }), page.click('[data-testid=intake-submit]')]);
  step('re-paste lands on the same Position', new URL(page.url()).pathname === pos, page.url());
  const real = errors.filter(e => !/status of 400/.test(e)); // the one deliberate 400 above
  step('no page errors in the browser console', real.length === 0, real.join(' | '));
  await browser.close();
  process.exit(failed ? 1 : 0);
})().catch(e => { console.error('FAIL harness :: ' + e.stack); process.exit(1); });
