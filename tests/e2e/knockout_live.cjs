// JSK-T177 approval-screen knockout checklist, live walk: real jobseekerd, real Chromium, virtual passkey. DRY-RUN only: nothing is sent.
// See knockout_live_test.go for the evidence class (synthetic postings and overlay, real server and browser).
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
  const txt = async sel => (await page.locator(sel).first().innerText()).trim();
  const area = async () => { const c = page.locator('[data-testid=area-confirm]'); if ((await c.count()) && !(await c.isChecked())) await c.check(); };
  const toApproval = async id => {
    await page.goto(base + '/postings/' + id);
    await page.getByText('Shortlist and start an application').click({ timeout: 20000 });
    await page.waitForURL(/\/applications\/\d+\/draft/, { timeout: 10000 });
    const app = page.url().match(/applications\/(\d+)/)[1];
    await page.getByText('Prepare template draft').click({ timeout: 20000 });
    step('posting ' + id + ' reaches awaiting_human_approval', (await txt('[data-testid=app-state]')) === 'awaiting_human_approval', await txt('[data-testid=app-state]'));
    return app;
  };

  // posting 2 names US: both questions are unfavourable, both open, both carry a checkbox
  const app2 = await toApproval(2);
  step('the knockout checklist is on the approval page', (await page.locator('[data-testid=knockout]').count()) === 1);
  step('the work-authorisation row is listed as not met', (await txt('[data-testid=ko-work_auth_required]')).includes('your profile does not meet it'), await txt('[data-testid=ko-work_auth_required]'));
  step('the sponsorship row is listed as not met', (await txt('[data-testid=ko-sponsorship_restriction]')).includes('your profile does not meet it'));
  step('each open row has an unticked acknowledgement checkbox named ack_knockout', (await page.locator('input[name=ack_knockout]').count()) === 2 && (await page.locator('input[name=ack_knockout]:checked').count()) === 0);
  step('checkbox labels are bound (each input sits in a label)', (await page.locator('[data-testid=knockout] label input[name=ack_knockout]').count()) === 2);
  step('no missing i18n key', !(await page.content()).includes('!!'));
  await page.screenshot({ path: path.join(ev, 'knockout-unfavourable-light.png'), fullPage: true });
  await axe(page, 'approval-knockout', 'light'); await axe(page, 'approval-knockout', 'dark');
  await page.screenshot({ path: path.join(ev, 'knockout-unfavourable-dark.png'), fullPage: true });
  await page.emulateMedia({ colorScheme: 'light' });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: path.join(ev, 'knockout-unfavourable-mobile.png'), fullPage: true });
  step('mobile: no horizontal page scroll', await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1));
  await page.setViewportSize({ width: 1280, height: 900 });
  // keyboard only: Tab reaches the first checkbox and Space ticks it
  await page.locator('#company').focus();
  let reached = false;
  await page.keyboard.press('Shift+Tab');
  for (let i = 0; i < 6 && !reached; i++) { reached = await page.evaluate(() => document.activeElement && document.activeElement.getAttribute('name') === 'ack_knockout'); if (!reached) await page.keyboard.press('Shift+Tab'); }
  if (reached) await page.keyboard.press('Space');
  step('keyboard only: a knockout acknowledgement is reached with Tab and ticked with Space', reached && (await page.locator('input[name=ack_knockout]:checked').count()) === 1);
  await page.locator('[data-testid=ack-knockout-work_auth_required]').check();
  await page.locator('[data-testid=ack-knockout-sponsorship_restriction]').check();
  await page.fill('#company', 'Borealis Labs'); await area();
  await page.check('input[name=live][value=""]');
  await page.screenshot({ path: path.join(ev, 'knockout-acknowledged.png'), fullPage: true });
  await page.click('[data-testid=approve]');
  step('dry-run approval with both acknowledgements records dry_run_recorded', (await txt('[data-testid=app-state]')) === 'dry_run_recorded', await txt('[data-testid=app-state]'));

  // posting 3 names no country: cannot be checked, still open
  const app3 = await toApproval(3);
  step('an unknown posting country is shown as cannot be checked', (await txt('[data-testid=ko-work_auth_required]')).includes('cannot be checked'), await txt('[data-testid=ko-work_auth_required]'));
  await page.screenshot({ path: path.join(ev, 'knockout-missing.png'), fullPage: true });
  // RTL and non-Latin: the checklist renders in Arabic (dir=rtl) and Serbian without the layout breaking
  for (const lang of ['ar', 'sr']) {
    await page.setExtraHTTPHeaders({ 'Accept-Language': lang });
    await page.goto(base + '/applications/' + app3 + '/draft');
    const meta = await page.evaluate(() => ({ lang: document.documentElement.lang, dir: document.documentElement.dir, w: document.documentElement.scrollWidth <= window.innerWidth + 1 }));
    step('draft page renders in ' + lang + ' (html lang/dir ' + meta.lang + '/' + meta.dir + ') without horizontal scroll', meta.lang === lang && meta.w, JSON.stringify(meta));
    step('no untranslated knockout key shown in ' + lang, !(await page.content()).includes('knockout.') && !(await page.content()).includes('!!'));
    await page.screenshot({ path: path.join(ev, 'knockout-' + lang + '.png'), fullPage: true });
  }
  await page.setExtraHTTPHeaders({});
  await page.goto(base + '/applications/' + app3 + '/draft');
  // an unacknowledged dry-run is still allowed (dry-run never blocks): it records the advisory, nothing is refused
  await page.fill('#company', 'Cirrus Works'); await area(); await page.check('input[name=live][value=""]'); await page.click('[data-testid=approve]');
  step('dry-run approval WITHOUT acknowledgement is not blocked (advisory only)', (await txt('[data-testid=app-state]')) === 'dry_run_recorded', await txt('[data-testid=app-state]'));

  // posting 1 names DE, the synthetic overlay is authorised there: confirmed, no checkbox to tick
  await toApproval(1);
  step('a confirmed question is listed as confirmed', (await txt('[data-testid=ko-work_auth_required]')).includes('confirmed in your profile'), await txt('[data-testid=ko-work_auth_required]'));
  step('a confirmed question carries no acknowledgement checkbox', (await page.locator('input[name=ack_knockout]').count()) === 0);
  await page.screenshot({ path: path.join(ev, 'knockout-confirmed.png'), fullPage: true });

  step('no page errors in the browser console', errors.length === 0, errors.join(' | '));
  await browser.close();
  process.exit(failed ? 1 : 0);
})().catch(e => { console.error('FAIL harness :: ' + e.stack); process.exit(1); });
