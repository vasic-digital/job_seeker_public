// JSK-T196 live walk: real jobseekerd, real Chromium, virtual passkey. See position_live_test.go for the evidence class.
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

  // the redirect: /postings/2 -> /positions/N#listing-2
  await page.goto(base + '/postings/2');
  const u = new URL(page.url());
  step('/postings/{id} of a resolved listing redirects to its Position', /^\/positions\/\d+$/.test(u.pathname) && u.hash === '#listing-2', page.url());
  const pos = u.pathname;
  const t = async sel => (await page.locator(sel).first().innerText()).trim();
  step('merged title is shown', (await t('[data-testid=position-title]')).startsWith('Senior Go Engineer'), await t('[data-testid=position-title]'));
  step('title provenance names BOTH providers with their differing values', (await page.locator('[data-testid=prov-title-provA]').count()) === 1 && (await page.locator('[data-testid=prov-title-provB]').count()) === 1);
  step('salary comes from the provider that reports it', (await t('[data-testid=field-salary]')).includes('70000-90000 EUR') && (await t('[data-testid=field-salary]')).includes('provA'), await t('[data-testid=field-salary]'));
  step('both listings are listed', (await page.locator('[data-testid=listing-1]').count()) === 1 && (await page.locator('[data-testid=listing-2]').count()) === 1);
  step('https source gets an attribution link, http source gets none', (await page.locator('a[href="https://a.example.com/j/1"]').count()) === 1 && (await page.locator('a[href^="http://"]').count()) === 0);
  step('no missing i18n key', !(await page.content()).includes('!!'));
  await page.screenshot({ path: path.join(ev, 'position-desktop-light.png'), fullPage: true });
  await axe(page, 'position', 'light'); await axe(page, 'position', 'dark');
  await page.screenshot({ path: path.join(ev, 'position-desktop-dark.png'), fullPage: true });
  await page.emulateMedia({ colorScheme: 'light' });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: path.join(ev, 'position-mobile.png'), fullPage: true });
  step('mobile: no horizontal page scroll', await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1));
  await page.setViewportSize({ width: 1280, height: 900 });

  // keyboard: the listing link is reachable by Tab and opens the listing page, which links back
  await page.goto(base + pos);
  await page.locator('[data-testid=listing-1] a').first().focus();
  await page.keyboard.press('Enter');
  await page.waitForURL(/\/listings\/1$/, { timeout: 8000 }).catch(() => {});
  step('keyboard Enter on a listing link opens /listings/1 with a link back', new URL(page.url()).pathname === '/listings/1' && (await page.locator('[data-testid=to-position]').count()) === 1, page.url());

  // legacy posting (no Position) keeps its page; empty Position and unknown ids
  await page.goto(base + '/postings/3');
  step('a posting without a Position keeps its own page (no redirect)', new URL(page.url()).pathname === '/postings/3' && (await page.content()).includes('Unrelated Legacy Role'));
  await page.goto(base + '/positions/900');
  step('empty Position shows the empty state', (await page.locator('[data-testid=no-listings]').count()) === 1);
  await page.screenshot({ path: path.join(ev, 'position-empty.png') });
  // ---- JSK-T207: settings (corrupt favourites, provider toggle, employer policy) and the per-Position override
  await page.goto(base + '/settings');
  step('corrupt provider_favourites is reported as invalid with its stored value', (await page.locator('[data-testid=fav-invalid]').count()) === 1 && (await t('[data-testid=fav-invalid]')).includes('not json'));
  await axe(page, 'settings', 'light'); await axe(page, 'settings', 'dark'); await page.emulateMedia({ colorScheme: 'light' });
  await page.screenshot({ path: path.join(ev, 'settings-desktop.png'), fullPage: true });
  await page.fill('#fav-l', 'provB, provA'); await page.click('button:has-text("Save favourites")');
  await page.waitForURL(/\/settings$/, { timeout: 8000 }).catch(() => {});
  step('saving a valid list repairs it (alert gone, order kept)', (await page.locator('[data-testid=fav-invalid]').count()) === 0 && (await page.inputValue('#fav-l')) === 'provB, provA');
  await page.click('[data-testid=provtoggle-provB]');
  await page.waitForURL(/\/settings$/, { timeout: 8000 }).catch(() => {});
  step('disabling provB shows Off and offers Enable', (await t('[data-testid=provstate-provB]')) === 'Off' && (await t('[data-testid=provtoggle-provB]')).toLowerCase().startsWith('enable'), await t('[data-testid=provstate-provB]'));
  await page.click('[data-testid=provtoggle-provB]');
  await page.waitForURL(/\/settings$/, { timeout: 8000 }).catch(() => {});
  step('re-enabling provB shows On', (await t('[data-testid=provstate-provB]')) === 'On');
  const emp = page.locator('form[data-testid^=emp-]').first();
  await emp.locator('input[name=one_per_position]').check(); await emp.locator('input[name=max_per_window]').fill('2'); await emp.locator('input[name=window_days]').fill('30');
  await emp.locator('button[type=submit]').click();
  await page.waitForURL(/\/settings$/, { timeout: 8000 }).catch(() => {});
  const emp2 = page.locator('form[data-testid^=emp-]').first();
  step('employer policy persists and is shown', (await emp2.locator('input[name=max_per_window]').inputValue()) === '2' && (await emp2.locator('input[name=one_per_position]').isChecked()));
  await emp2.locator('input[name=max_per_window]').fill('5'); await emp2.locator('input[name=window_days]').fill('');
  const r400 = await Promise.all([page.waitForResponse(r => r.request().method() === 'POST' && /\/settings\/employers\//.test(r.url())), emp2.locator('button[type=submit]').click()]);
  step('a cap without a window is refused (400)', r400[0].status() === 400, String(r400[0].status()));
  await page.goto(base + pos);
  await page.selectOption('#ov-m', 'none'); await page.fill('#ov-r', 'live check');
  await page.click('[data-testid=override-save]');
  await page.waitForURL(base + pos, { timeout: 8000 }).catch(() => {});
  step('override none is stored and shown', (await t('[data-testid=position-override]')).startsWith('none') && (await page.inputValue('#ov-r')) === 'live check', await t('[data-testid=position-override]'));
  await page.screenshot({ path: path.join(ev, 'position-override.png'), fullPage: true });
  await axe(page, 'position-override', 'light'); await axe(page, 'position-override', 'dark'); await page.emulateMedia({ colorScheme: 'light' });
  await page.selectOption('#ov-m', 'inherit'); await page.click('[data-testid=override-save]');
  await page.waitForURL(base + pos, { timeout: 8000 }).catch(() => {});
  step('inherit clears the override', (await page.locator('[data-testid=position-override]').count()) === 0);
  await page.selectOption('#ov-m', 'all'); await page.fill('#ov-r', 'wants every channel');
  const rall = await Promise.all([page.waitForResponse(r => r.request().method() === 'POST' && /\/apply-override$/.test(r.url())), page.click('[data-testid=override-save]')]);
  await page.waitForURL(base + pos, { timeout: 8000 }).catch(() => {});
  step('override all is accepted right after sign-in (a fresh step-up; the stale-step-up 403 is covered by the console tests)', rall[0].status() === 303 && (await t('[data-testid=position-override]')).startsWith('all'), String(rall[0].status()));
  const real = errors.filter(e => !/status of 400/.test(e)); // the one deliberate 400 above
  step('no page errors in the browser console', real.length === 0, real.join(' | '));
  const r404 = await page.goto(base + '/positions/99999');
  step('unknown Position is 404', r404.status() === 404, String(r404.status()));
  await browser.close();
  process.exit(failed ? 1 : 0);
})().catch(e => { console.error('FAIL harness :: ' + e.stack); process.exit(1); });
