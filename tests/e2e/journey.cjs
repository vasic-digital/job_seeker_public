// Console journey, HTTP-level refusal evidence, accessibility sweep and keyboard/zoom walk against a real jobseekerd
// (see e2e_test.go for the evidence class). Phases: browse + triage, draft + dry-run approval, settings, tracker,
// every FR-026 bypass code over HTTP (T068), keyboard-only walk and 200% zoom (T080), the full live journey with the
// canary overlay (T089).
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
const E = process.env, base = E.BASE_URL, ev = E.EVIDENCE_DIR, sums = JSON.parse(E.TEMPLATE_SUMS);
const seed = JSON.parse(E.SEED_INFO), canaries = JSON.parse(E.CANARIES);
const axeSrc = fs.readFileSync(E.AXE_JS, 'utf8');
const log = []; let failed = 0;
const { gluedEmails } = require('./glue.cjs');
function step(name, ok, detail) { if (gluedEmails(name + ' ' + (detail || '')).length) { ok = false; detail = 'CAPTURE DEFECT: e-mail glued to following text (cell separators lost) :: ' + detail; } log.push((ok ? 'PASS ' : 'FAIL ') + name + (detail ? ' :: ' + detail : '')); console.log(log[log.length - 1]); if (!ok) failed++; }
function note(name, detail) { log.push('NOTE ' + name + (detail ? ' :: ' + detail : '')); console.log(log[log.length - 1]); }
const args = ['--host-resolver-rules=MAP js.lan 127.0.0.1', '--ignore-certificate-errors'];
const sleep = ms => new Promise(r => setTimeout(r, ms));

async function axeRun(page, name, scheme) {
  await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
  await page.evaluate(axeSrc);
  const r = await page.evaluate(async () => await axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'] } }));
  const v = r.violations.map(x => x.id + '(' + x.nodes.length + '): ' + x.nodes.slice(0, 2).map(n => n.target.join(' ')).join(' | '));
  step('axe ' + name + ' ' + scheme, v.length === 0, v.join('; ') || (r.passes.length + ' rules passed'));
  fs.writeFileSync(path.join(ev, `axe-${name}-${scheme}.json`), JSON.stringify({ url: page.url(), violations: r.violations.length, passes: r.passes.length, incomplete: r.incomplete.length }, null, 1));
}
async function axeBoth(page, name) { for (const s of ['light', 'dark']) await axeRun(page, name, s); await page.emulateMedia({ colorScheme: 'light' }); }
const pageFetch = (page, url, opts) => page.evaluate(async ([u, o]) => { const r = await fetch(u, Object.assign({ credentials: 'same-origin' }, o || {})); const b = await r.arrayBuffer();
  const d = await crypto.subtle.digest('SHA-256', b); return { status: r.status, headers: Object.fromEntries([...r.headers.entries()]), sha: [...new Uint8Array(d)].map(x => x.toString(16).padStart(2, '0')).join(''), text: new TextDecoder().decode(b) }; }, [url, opts]);
const txt = async (page, sel) => (await page.locator(sel).first().innerText()).trim();
const errCode = async page => { const l = page.locator('[data-error-code]').first(); return (await l.count()) ? await l.getAttribute('data-error-code') : ''; };

// A hover transform on a card that holds the pointer's own hit area is a feedback loop: the card moves out from under the pointer, loses :hover,
// moves back, regains it, forever. Playwright then never sees the card (or the button inside it) as stable. The probe parks the pointer on the card,
// moves it to 3px below the settled card and samples the card's position: it must hold still.
async function cardHoldsStill(pg) {
  const card = pg.locator('section.console-card').first();
  const c0 = await card.boundingBox();
  await pg.mouse.move(c0.x + c0.width / 2, c0.y + c0.height / 2);
  await pg.waitForTimeout(700);
  const c1 = await card.boundingBox();
  await pg.mouse.move(c1.x + c1.width / 2, c1.y + c1.height + 3);
  const seen = new Set();
  for (let i = 0; i < 30; i++) { const b = await card.boundingBox(); seen.add(b.x.toFixed(1) + ',' + b.y.toFixed(1)); await pg.waitForTimeout(50); }
  await pg.mouse.move(0, 0);
  return [...seen];
}

(async () => {
  const browser = await chromium.launch({ args });
  const ctx = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 900 } });
  const page = await ctx.newPage();
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('WebAuthn.enable');
  await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
  const errors = []; page.on('pageerror', e => errors.push(String(e)));
  const consoleBad = []; page.on('console', m => { if (m.type() === 'error') consoleBad.push(m.text()); });
  // T089: every textual HTTP response body the browser receives is checked for the synthetic overlay values
  const canaryHits = []; let bodiesChecked = 0;
  page.on('response', async r => { try { const ct = r.headers()['content-type'] || ''; if (!/text|json|xml/.test(ct)) return; const b = await r.text(); bodiesChecked++;
    for (const c of canaries) if (b.includes(c)) canaryHits.push(new URL(r.url()).pathname); } catch (e) { /* redirect or aborted body */ } });
  const refusals = [];
  // JSK-T187b: when the approval report demands the CV area overlap be confirmed, Approve is refused without the tick
  async function ackArea(pg) { const c = pg.locator('[data-testid=area-confirm]'); if ((await c.count()) && !(await c.isChecked())) await c.check(); }
  async function refuse(clickSel, want, wantStatus, label, stateSel) {
    if (clickSel === '[data-testid=approve]') await ackArea(page);
    const [resp] = await Promise.all([page.waitForResponse(r => r.request().method() === 'POST' && /\/(approve|handoff-complete)$/.test(new URL(r.url()).pathname), { timeout: 15000 }), page.click(clickSel)]);
    const code = await errCode(page);
    refusals.push({ code, http_status: resp.status(), endpoint: new URL(resp.url()).pathname.replace(/\/\d+\//, '/{id}/'), label });
    step('HTTP ' + wantStatus + ' ' + want + ' at ' + label, code === want && resp.status() === wantStatus, 'got ' + resp.status() + ' ' + code);
  }
  let lastStepUp = 0;
  async function stepUpCeremony() {
    await page.click('#stepup');
    await page.waitForFunction(() => document.getElementById('passkey-status').textContent === 'ok', null, { timeout: 15000 }).catch(() => {});
    const ok = (await txt(page, '#passkey-status')) === 'ok'; if (ok) lastStepUp = Date.now(); return ok;
  }

  // ---- sign-in
  await page.goto(base + '/');
  step('unauthenticated browser navigation lands on /login', new URL(page.url()).pathname === '/login', page.url());
  await axeBoth(page, 'login');
  await page.fill('#bootstrap-token', E.BOOTSTRAP_TOKEN);
  await page.click('#passkey-register');
  await page.waitForFunction(() => document.getElementById('passkey-status').textContent === 'ok', null, { timeout: 15000 }).catch(() => {});
  step('first passkey registered (virtual authenticator)', (await txt(page, '#passkey-status')) === 'ok');
  await page.click('#passkey-login');
  await page.waitForURL(base + '/', { timeout: 15000 }).catch(() => {});
  lastStepUp = Date.now();
  step('passkey sign-in reaches the inbox', new URL(page.url()).pathname === '/' && (await page.title()).startsWith('Inbox'), await page.title());

  // ---- inbox: synthetic persona rows, htmx filters, keyboard shortcut
  await page.goto(base + '/?provider=remotive');
  step('inbox lists the 8 persona postings of provider remotive', (await page.locator('tbody tr').count()) === 8);
  const htmx = page.waitForResponse(r => r.url().includes('/?') && r.request().headers()['hx-request'] === 'true', { timeout: 8000 }).catch(() => null);
  await page.selectOption('#f-state', 'unmapped');
  const hr = await htmx;
  await page.waitForFunction(() => document.querySelectorAll('tbody tr').length === 1, null, { timeout: 8000 }).catch(() => {});
  step('state filter narrows the list through htmx (hx-request seen)', !!hr && (await page.locator('tbody tr').count()) === 1);
  await page.selectOption('#f-state', '');
  await page.waitForFunction(() => document.querySelectorAll('tbody tr').length === 7, null, { timeout: 8000 }).catch(() => {});
  await page.keyboard.press('Escape'); await page.focus('body');
  await page.keyboard.press('j');
  step('keyboard shortcut j focuses the first posting', await page.evaluate(() => document.activeElement && document.activeElement.hasAttribute('data-kbd-row')));
  await axeBoth(page, 'inbox');

  // ---- real postings through the real classifier, scorer and matchstore (only when the private corpus is present)
  if (seed.real_present) {
    await page.goto(base + '/?provider=gold-corpus');
    const total = await page.locator('tbody tr').count();
    step('inbox shows every real corpus posting that fits the 200-row page', total === Math.min(200, seed.real_count), total + ' rows of ' + seed.real_count);
    let bands = 0;
    for (const b of ['shortlist', 'review', 'discard']) { await page.goto(base + '/?provider=gold-corpus&band=' + b); bands += await page.locator('tbody tr').count(); }
    step('score bands partition exactly the scored real postings', bands === seed.real_scored, bands + ' vs ' + seed.real_scored);
    await page.goto(base + '/postings/' + seed.real_first_scored);
    const body = await page.content();
    step('real posting detail shows the real score and a readable breakdown (sealed columns open)', body.includes('Score ' + Math.round(seed.real_first_total) + ' of 100') && (await page.locator('table tbody tr').count()) >= 2, 'rows=' + await page.locator('table tbody tr').count());
    await axeBoth(page, 'job-real');
  } else note('real corpus absent on this host', 'real-posting steps skipped, labelled; synthetic rows only');

  // ---- dismiss (persona posting 7, scored)
  await page.goto(base + '/postings/' + 7);
  await page.click('[data-testid=dismiss]');
  await page.waitForURL(base + '/', { timeout: 8000 }).catch(() => {});
  await page.goto(base + '/postings/' + 7);
  step('dismiss moves a scored posting to discarded and removes the actions', (await page.content()).includes('discarded') && (await page.locator('[data-testid=dismiss]').count()) === 0);
  const again = await pageFetch(page, '/postings/' + 7 + '/dismiss', { method: 'POST', headers: { 'X-CSRF-Token': await page.locator('meta[name=csrf-token]').getAttribute('content') } });
  step('dismissing a posting that is no longer scored is refused (409 illegal_transition)', again.status === 409 && again.text.includes('illegal_transition'), String(again.status));

  // ---- review queue: matcher evidence + matchstore.MapHuman
  await page.goto(base + '/queue'); await axeBoth(page, 'queue');
  const q0 = await page.locator('tbody tr').count();
  step('review queue lists the unmapped persona posting' + (seed.real_present ? ' and the real unmapped ones' : ''), q0 === 1 + (seed.real_present ? seed.real_unmapped : 0), 'rows=' + q0);
  step('every queue row carries the matcher evidence cell (closest areas, hits)', (await page.locator('[data-testid=top3]').count()) >= 1, 'evidence cells=' + await page.locator('[data-testid=top3]').count());
  step('the map form offers exactly the 10 legal areas', (await page.locator('#area-5 option').count()) === 10);
  await page.selectOption('#area-5', 'android'); await page.click('tr:has-text("Unclear Role") button:has-text("Map to area")');
  await page.waitForLoadState('load');
  step('manual map moves the posting out of the queue (matchstore.MapHuman, audited mapped_by_human)', (await page.locator('tr:has-text("Unclear Role")').count()) === 0 && (await page.locator('tbody tr').count()) === q0 - 1);
  const stale = await pageFetch(page, '/queue/5/map', { method: 'POST', headers: { 'X-CSRF-Token': await page.locator('meta[name=csrf-token]').getAttribute('content'), 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'area_id=android' });
  step('mapping an already mapped posting is refused (409 illegal_transition)', stale.status === 409 && stale.text.includes('illegal_transition'), String(stale.status));
  const bad = await pageFetch(page, '/queue/' + (seed.real_present ? seed.real_queued : 5) + '/map', { method: 'POST', headers: { 'X-CSRF-Token': await page.locator('meta[name=csrf-token]').getAttribute('content'), 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'area_id=kotlin-multiplatform' });
  step('an area outside the 10 legal ones is refused (400 validation)', bad.status === 400 && bad.text.includes('validation'), String(bad.status));

  // ---- job detail
  await page.goto(base + '/postings/1');
  step('job detail shows score breakdown and sanitized text', (await page.content()).includes('Score 82 of 100') && (await page.content()).includes('Build services in Go.'));
  step('attribution link is noreferrer noopener', (await page.locator('a[rel="noreferrer noopener"]').count()) === 1);
  await axeBoth(page, 'job');

  // ---- shortlist -> draft -> prepare -> dry-run approval
  await page.click('text=Shortlist and start an application');
  await page.waitForURL(/\/applications\/\d+\/draft/, { timeout: 8000 }).catch(async () => { console.log('DEBUG url', page.url(), (await page.content()).slice(0, 600)); });
  const app1 = page.url().match(/applications\/(\d+)/)[1];
  step('shortlist creates a pending_draft application', (await txt(page, '[data-testid=app-state]')) === 'pending_draft');
  await page.click('text=Prepare template draft');
  step('prepare freezes the draft: awaiting_human_approval with a hash', (await txt(page, '[data-testid=app-state]')) === 'awaiting_human_approval' && (await txt(page, '[data-testid=draft-hash]')).length === 64);
  const shown = await txt(page, '[data-testid=sha-cv]');
  step('displayed CV SHA-256 equals the file manifest hash', shown === sums['cv-go-en.pdf']);
  const appURL = page.url();
  const fr = await pageFetch(page, appURL.replace(/draft$/, 'files/cv'));
  const h = fr.headers;
  step('PDF streamed with no-store, nosniff, inline and the same hash',
    fr.status === 200 && h['cache-control'] === 'no-store' && h['x-content-type-options'] === 'nosniff' && h['content-disposition'].startsWith('inline') && fr.sha === shown, JSON.stringify({ s: fr.status, cc: h['cache-control'] }));
  const rg = await pageFetch(page, appURL.replace(/draft$/, 'files/cv'), { headers: { Range: 'bytes=0-9' } });
  step('PDF viewer serves ranges from the tmpfs-staged copy (206, 10 bytes)', rg.status === 206 && rg.text.length === 10 && rg.headers['cache-control'] === 'no-store', String(rg.status));
  await axeBoth(page, 'draft');

  // JSK-T187 (FR-108): the approval report is on the page for this exact draft, Approve is enabled only with it, and a post without its digest is refused
  const repSel = '[data-testid=approval-report]';
  step('the approval report is rendered on the approval page (T187)', (await page.locator(repSel).count()) === 1);
  step('report shows the company and the draft date', (await txt(page, '[data-testid=report-company]')) === 'Acme Robotics' && /^\d{4}-\d{2}-\d{2}$/.test(await txt(page, '[data-testid=report-date]')), await txt(page, '[data-testid=report-date]'));
  step('report shows the link table state, the text diff state and the coverage counts', (await page.locator('[data-testid=report-links],[data-testid=report-links-none]').count()) === 1 && (await page.locator('[data-testid=report-diff],[data-testid=report-diff-unavailable]').count()) === 1 && /posting terms are in the CV/.test(await txt(page, '[data-testid=report-coverage]')), await txt(page, '[data-testid=report-coverage]'));
  step('Approve is enabled while the report is rendered', !(await page.locator('[data-testid=approve]').isDisabled()));
  await page.locator(repSel).screenshot({ path: path.join(ev, 'approval-report.png') });
  const noRep = await pageFetch(page, appURL.replace(/draft$/, 'approve'), { method: 'POST', headers: { 'X-CSRF-Token': await page.$eval('input[name=_csrf]', el => el.value), 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'company=Acme+Robotics' });
  step('HTTP 409 report_required at POST /applications/{id}/approve without the report digest (T187)', noRep.status === 409 && noRep.text.includes('report_required'), String(noRep.status));

  // JSK-T187b: the persona posting demands the area confirmation; a post that carries the current digest but no confirmation is refused
  step('the approval form offers the area-overlap confirmation the report demands (T187b)', (await page.locator('[data-testid=report-area-confirm]').count()) === 1 && (await page.locator('[data-testid=area-confirm]').count()) === 1);
  const noAck = await pageFetch(page, appURL.replace(/draft$/, 'approve'), { method: 'POST', headers: { 'X-CSRF-Token': await page.$eval('input[name=_csrf]', el => el.value), 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'company=Acme+Robotics&report=' + encodeURIComponent(await page.$eval('input[name=report]', el => el.value)) });
  step('HTTP 409 area_confirmation_required at POST /applications/{id}/approve with the digest but without area_confirm (T187b)', noAck.status === 409 && noAck.text.includes('area_confirmation_required'), String(noAck.status));
  await page.locator('[data-testid=area-confirm]').check();
  await page.screenshot({ path: path.join(ev, 'approval-area-confirm.png'), fullPage: true });
  await page.check('input[name=live][value="1"]'); await page.fill('#company', 'Acme Robotics');
  await refuse('[data-testid=approve]', 'live_mode_off', 403, 'POST /applications/{id}/approve');
  await page.check('input[name=live][value=""]');
  await ackArea(page); await page.click('[data-testid=approve]');
  step('dry-run approval records dry_run_recorded', (await txt(page, '[data-testid=app-state]')) === 'dry_run_recorded');

  // ---- applications list + settings (kill switch, live toggle needs step-up), legal diff, retention
  await page.goto(base + '/applications'); await axeBoth(page, 'applications');
  await page.goto(base + '/settings');
  step('settings show live mode off and kill switch off', (await txt(page, '[data-testid=live-state]')) === 'Off' && (await txt(page, '[data-testid=kill-state]')) === 'Off');
  step('settings show the legal-notice diff text (T077, FR-007)', (await txt(page, '[data-testid=legal-diff]')).includes('+ Commercial use needs written consent.') && (await page.locator('[data-testid=legal-remotive]').count()) === 1);
  step('settings show retention in force and the last prune (T077, C9)', (await txt(page, '[data-testid=retention-postings]')).includes('90 days') && (await txt(page, '[data-testid=retention-snapshots]')).includes('14 days') && (await txt(page, '[data-testid=last-prune]')).length > 0, await txt(page, '[data-testid=last-prune]'));
  await page.screenshot({ path: path.join(ev, 'settings-legal-retention.png'), fullPage: true });
  step('passkey step-up ceremony succeeds', await stepUpCeremony());
  await page.click('[data-testid=live-toggle]');
  step('with a fresh step-up live mode turns on', (await txt(page, '[data-testid=live-state]')) === 'On');
  await page.goto(base + '/applications/' + app1 + '/draft');
  step('draft page shows live mode on', (await page.content()).includes('Live'));
  await page.goto(base + '/settings');
  await page.click('[data-testid=kill-toggle]');
  step('kill switch turns on without step-up', (await txt(page, '[data-testid=kill-state]')) === 'On');
  await axeBoth(page, 'settings');
  await page.click('[data-testid=kill-toggle]');
  step('kill switch turns off again with a fresh step-up', (await txt(page, '[data-testid=kill-state]')) === 'Off');
  await page.click('[data-testid=live-toggle]');
  step('live mode turns off again', (await txt(page, '[data-testid=live-state]')) === 'Off');

  // ---- tracker page (T086): timeline, concurrent-edit conflict, follow-up due surfaced in the inbox
  await page.goto(base + '/applications/' + app1 + '/tracking'); await axeBoth(page, 'tracking');
  step('tracker page starts empty', (await page.locator('[data-testid=timeline]').count()) === 0);
  await page.selectOption('#tr-status', 'sent'); await page.fill('#tr-note', 'Applied through the provider page'); await page.click('[data-testid=track-save]');
  step('recording a status shows it in the timeline with its decrypted note', (await page.locator('[data-testid=timeline]').count()) === 1 && (await txt(page, '[data-testid=timeline]')).includes('Applied through the provider page'), 'err=' + await errCode(page) + ' url=' + page.url());
  await page.selectOption('#tr-status', 'viewed'); await page.evaluate(() => { document.querySelector('input[name=latest]').value = '0'; });
  await page.click('[data-testid=track-save]');
  step('a stale timeline edit is refused (409, no lost update)', (await errCode(page)) === 'illegal_transition');
  await page.goto(base + '/applications/' + app1 + '/tracking');
  const yday = new Date(Date.now() - 86400000).toISOString().slice(0, 10);
  await page.fill('#fu-date', yday); await page.click('[data-testid=followup-save]');
  step('follow-up date saved and shown back on the tracker page', (await page.inputValue('#fu-date')) === yday, 'err=' + await errCode(page) + ' value=' + await page.inputValue('#fu-date'));
  await page.goto(base + '/');
  step('a follow-up that has arrived is surfaced in the inbox', (await page.locator('[data-testid=due-followups]').count()) === 1 && (await txt(page, '[data-testid=due-followups]')).toLowerCase().includes('senior go engineer'), 'url=' + page.url() + ' due-count=' + await page.locator('[data-testid=due-followups]').count() + ' main=' + (await txt(page, 'main')).replace(/\s+/g, ' ').slice(0, 200));
  await axeBoth(page, 'inbox-due');
  await page.goto(base + '/applications/' + app1 + '/tracking');
  await page.selectOption('#tr-status', 'offer'); await page.click('[data-testid=track-save]');
  await page.goto(base + '/');
  step('a terminal status (offer) stops the reminder', (await page.locator('[data-testid=due-followups]').count()) === 0);

  // ---- German posting: language confirmation gate (code language_confirmation_required)
  await page.goto(base + '/postings/3');
  await page.click('text=Shortlist and start an application'); await page.waitForURL(/draft/);
  await page.click('text=Prepare template draft');
  step('German posting requires language confirmation', (await txt(page, '[data-testid=app-lang]')).includes('confirm_de'));
  await refuse('[data-testid=approve]', 'language_confirmation_required', 400, 'POST /applications/{id}/approve');
  await page.selectOption('#lang', 'en'); await page.click('text=Confirm language');
  step('confirming the language re-freezes the draft', (await txt(page, '[data-testid=app-lang]')).includes('human_confirmed') && (await txt(page, '[data-testid=app-state]')) === 'awaiting_human_approval');
  await page.evaluate(() => document.getElementById('reason').removeAttribute('required'));
  await page.click('[data-testid=reject]');
  step('reject without a reason is refused', (await errCode(page)) === 'validation');
  await page.fill('#reason', 'Not a fit'); await page.click('[data-testid=reject]');
  step('reject with a reason records rejected_by_human', (await txt(page, '[data-testid=app-state]')) === 'rejected_by_human');

  // ---- T068: every other FR-026 bypass code over HTTP, on one application, with the synthetic overlay (parked, then restored)
  const parked = path.join(E.OVERLAY_DIR, 'overlay.yaml.parked'), live = path.join(E.OVERLAY_DIR, 'overlay.yaml');
  await page.goto(base + '/settings');
  if (Date.now() - lastStepUp > 240000) await stepUpCeremony();
  await page.click('[data-testid=live-toggle]');
  step('live mode on for the bypass phase', (await txt(page, '[data-testid=live-state]')) === 'On');
  await page.goto(base + '/postings/4'); await page.click('text=Shortlist and start an application'); await page.waitForURL(/draft/);
  await page.click('text=Prepare template draft');
  const bypassURL = page.url(), bypassId = bypassURL.match(/applications\/(\d+)/)[1];
  const live1 = async company => { await page.goto(bypassURL); await page.check('input[name=live][value="1"]'); await page.fill('#company', company); };
  await live1('Delta Apps');
  await refuse('[data-testid=approve]', 'overlay_absent', 403, 'POST /applications/{id}/approve');
  step('refused attempt left the application awaiting approval', (await txt(page, '[data-testid=app-state]')) === 'awaiting_human_approval');
  fs.renameSync(parked, live);
  await page.goto(base + '/settings'); await page.click('[data-testid=kill-toggle]');
  await live1('Delta Apps');
  await refuse('[data-testid=approve]', 'kill_switch_on', 403, 'POST /applications/{id}/approve');
  await page.goto(base + '/settings'); await page.click('[data-testid=kill-toggle]');
  step('kill switch off again', (await txt(page, '[data-testid=kill-state]')) === 'Off');
  await live1('delta apps');
  await refuse('[data-testid=approve]', 'company_confirmation_failed', 403, 'POST /applications/{id}/approve');
  await page.goto(base + '/settings');
  const capD = await page.inputValue('#cap-d'), capH = await page.inputValue('#cap-h');
  await page.fill('#cap-d', '0'); await page.click('button:has-text("Save caps")');
  await live1('Delta Apps');
  await refuse('[data-testid=approve]', 'cap_exceeded', 403, 'POST /applications/{id}/approve');
  await page.goto(base + '/settings'); await page.fill('#cap-d', capD); await page.fill('#cap-h', capH); await page.click('button:has-text("Save caps")');
  step('caps restored', (await page.inputValue('#cap-d')) === capD && (await page.inputValue('#cap-h')) === capH);
  // JSK-T182: the hard gates are re-evaluated at live approval. The operator blocks the company AFTER it was shortlisted
  // (synthetic overlay edited on disk); the live approval is refused with its own closed code and audited, then restored.
  const ovOrig = fs.readFileSync(live);
  fs.writeFileSync(live, Buffer.concat([ovOrig, Buffer.from('  - Delta Apps\n')]));
  await live1('Delta Apps');
  await refuse('[data-testid=approve]', 'gate_regressed', 403, 'POST /applications/{id}/approve');
  step('the regressed hard gate left the application awaiting approval', (await txt(page, '[data-testid=app-state]')) === 'awaiting_human_approval');
  fs.writeFileSync(live, ovOrig);
  // JSK-T172: the overlay changed after the template set was built (its recorded overlay hash no longer matches): live approval refused
  fs.writeFileSync(live, Buffer.concat([ovOrig, Buffer.from('phone: synthetic-not-a-number\n')]));
  await live1('Delta Apps');
  await refuse('[data-testid=approve]', 'templates_stale', 409, 'POST /applications/{id}/approve');
  fs.writeFileSync(live, ovOrig);
  // approval_hash_mismatch: the CV template changes on disk after the draft was frozen
  const cvPath = path.join(E.TEMPLATE_DIR, 'cv-android-en.pdf'), cvOrig = fs.readFileSync(cvPath);
  fs.writeFileSync(cvPath, Buffer.concat([cvOrig, Buffer.from('\n% tampered after the draft was frozen\n')]));
  await live1('Delta Apps');
  // JSK-T187: the report cannot be produced for a draft whose file changed, so the Approve button is disabled; the server still refuses with its own code
  step('the approval report is unavailable for a changed draft and Approve is disabled (T187)', (await page.locator('[data-testid=approve]').isDisabled()) && (await page.locator('[data-testid=report-unavailable]').count()) === 1);
  await page.$eval('[data-testid=approve]', el => { el.removeAttribute('disabled'); el.removeAttribute('aria-disabled'); });
  await refuse('[data-testid=approve]', 'approval_hash_mismatch', 409, 'POST /applications/{id}/approve');
  step('the hash mismatch withdrew the approval path: the draft returns to draft_prepared', (await txt(page, '[data-testid=app-state]')) === 'draft_prepared');
  fs.writeFileSync(cvPath, cvOrig);
  await page.click('text=Prepare template draft');
  step('with the file restored, re-drafting freezes the same hash again (re-draft after hash change)', (await txt(page, '[data-testid=app-state]')) === 'awaiting_human_approval');
  // approve on an application that is not awaiting approval, and handoff on one that is not live_approved
  const csrf = await page.locator('meta[name=csrf-token]').getAttribute('content');
  const post = (u, body) => pageFetch(page, u, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/x-www-form-urlencoded' }, body });
  const hx = await post('/applications/' + app1 + '/handoff-complete', 'ref=SYNTH-REF');
  step('HTTP 409 illegal_transition at POST /applications/{id}/handoff-complete on an application that is not live_approved', hx.status === 409 && hx.text.includes('illegal_transition'), String(hx.status));
  refusals.push({ code: 'illegal_transition', http_status: hx.status, endpoint: '/applications/{id}/handoff-complete', label: 'handoff on a non-live_approved application' });
  const ax = await post('/applications/' + app1 + '/approve', 'live=1&company=Acme+Robotics');
  step('HTTP 409 illegal_transition at POST /applications/{id}/approve on an application that is not awaiting approval', ax.status === 409 && ax.text.includes('illegal_transition'), String(ax.status));

  // ---- JSK-T181: the same employer under another posting is refused, visibly, until the operator picks an audited reason
  await page.goto(base + '/postings/8'); await page.click('text=Shortlist and start an application');
  step('a second posting of an employer that already has an application is refused (duplicate_company)', (await page.content()).includes('duplicate_company'));
  await page.goto(base + '/postings/8');
  step('the refused posting offers the explicit override form', (await page.locator('[data-testid=override]').count()) === 1);
  await page.selectOption('[data-testid=override]', 'different_team'); await page.click('[data-testid=start-application]');
  await page.waitForURL(/draft/);
  step('with a closed override reason the application starts', /applications\/\d+\/draft/.test(page.url()), page.url());
  const dupURL = page.url();

  // ---- T080: keyboard-only walk, focus visibility, reduced motion, 200% zoom and reflow (runs while the step-up ages)
  await keyboardAndZoom();

  // ---- step_up_required: wait until the last passkey check is older than five minutes, then try a live approval
  const wait = lastStepUp + 301000 - Date.now();
  if (wait > 0) { note('waiting for the step-up to age past five minutes', Math.ceil(wait / 1000) + ' s (real clock, no test hook)'); await sleep(wait); }
  await live1('Delta Apps');
  await refuse('[data-testid=approve]', 'step_up_required', 403, 'POST /applications/{id}/approve');
  const codes = new Set(refusals.map(r => r.code));
  const eight = ['live_mode_off', 'overlay_absent', 'kill_switch_on', 'step_up_required', 'company_confirmation_failed', 'cap_exceeded', 'approval_hash_mismatch', 'language_confirmation_required'];
  step('all 8 FR-026 bypass codes were refused over HTTP in a real browser', eight.every(c => codes.has(c)), eight.filter(c => !codes.has(c)).join(',') || 'complete');
  fs.writeFileSync(path.join(ev, 'bypass-codes.json'), JSON.stringify(refusals, null, 1));

  // ---- the full live journey: fresh step-up, live approval, handoff re-check under the kill switch, handoff, sent
  await page.goto(base + '/settings'); step('fresh step-up for the live approval', await stepUpCeremony());
  await live1('Delta Apps'); await ackArea(page); await page.click('[data-testid=approve]');
  step('with every FR-026 condition satisfied the live approval is recorded (live_approved)', (await txt(page, '[data-testid=app-state]')) === 'live_approved');
  await page.goto(base + '/settings'); await page.click('[data-testid=kill-toggle]');
  await page.goto(bypassURL); await page.fill('#ref', 'SYNTH-CONFIRMATION-001');
  await refuse('button:has-text("Record as sent")', 'kill_switch_on', 403, 'POST /applications/{id}/handoff-complete');
  await page.goto(base + '/settings'); await page.click('[data-testid=kill-toggle]');
  await page.goto(bypassURL); await page.fill('#ref', 'SYNTH-CONFIRMATION-001'); await page.click('button:has-text("Record as sent")');
  step('the operator-recorded handoff is the only path to sent', (await txt(page, '[data-testid=app-state]')) === 'sent');
  await page.goto(base + '/settings'); await page.click('[data-testid=live-toggle]');
  step('live mode off at the end of the live journey', (await txt(page, '[data-testid=live-state]')) === 'Off');
  await page.goto(base + '/applications/' + bypassId + '/tracking');
  await page.selectOption('#tr-status', 'viewed'); await page.click('[data-testid=track-save]');
  step('a sent application is trackable (sent -> tracked)', (await txt(page, '[data-testid=timeline]')).toLowerCase().includes('viewed'), 'err=' + await errCode(page) + ' url=' + page.url());

  // ---- JSK-T187b: the approval report with REAL links (the template PDFs carry an https and a mailto annotation, extracted by the
  // real poppler parser) and a REAL prior send (the Delta Apps application recorded as sent above): both the link table and the text diff render
  await page.goto(dupURL); await page.click('text=Prepare template draft');
  step('the second application of the journey is awaiting approval with its report (T187b)', (await txt(page, '[data-testid=app-state]')) === 'awaiting_human_approval' && (await page.locator('[data-testid=approval-report]').count()) === 1);
  const linkRows = await page.locator('[data-testid=report-links] tbody tr').evaluateAll(trs => trs.map(tr => [...tr.cells].map(c => c.textContent.trim()).join(' | ')));
  step('report link table lists the real https and mailto links of both frozen files, all accepted (T187b)', linkRows.length === 4 && linkRows.filter(r => r.includes('https://example.com/p')).length === 2 && linkRows.filter(r => r.includes('mailto:a@example.com')).length === 2 && linkRows.every(r => /\| yes$/i.test(r.trim())), linkRows.map(r => r.replace(/\s+/g, ' ')).join(' | '));
  step('report text diff is computed against the CV of the most recent send (cv-go-en vs cv-android-en) with exact counts (T187b)', (await page.locator('[data-testid=report-diff]').count()) === 1 && (await txt(page, '[data-testid=report-diff-added]')).includes('cv-go-en.pdf') && (await txt(page, '[data-testid=report-diff-removed]')).includes('cv-android-en.pdf'), (await txt(page, '[data-testid=report-diff]')).replace(/\s+/g, ' '));
  await page.locator('[data-testid=approval-report]').screenshot({ path: path.join(ev, 'approval-report-links-diff.png') });
  await axeBoth(page, 'draft-report-links');

  // ---- audit log page and export: chain order and the reason codes the journey must have produced
  await page.goto(base + '/audit'); await axeBoth(page, 'audit');
  const jl = (await pageFetch(page, '/audit.jsonl')).text;
  const rows = jl.trim().split('\n').map(JSON.parse);
  step('audit export has the transitions in chain order', rows.length >= 8 && rows.every((r, i) => i === 0 || r.prev_hash === rows[i - 1].this_hash), rows.length + ' rows');
  const reasons = new Set(rows.map(r => r.reason));
  const want = ['mapped_by_human', 'dry_run_recorded', 'rejected_by_human', 'tracked', 'follow_up_set', 'live_approved', 'handoff_recorded', 'draft_hash_changed', 'kill_switch_on', 'kill_switch_off', 'live_mode_changed', 'dismissed_by_human', 'refused_gate_regressed', 'refused_duplicate_company', 'override_duplicate_company', 'refused_templates_stale'].concat(seed.real_present ? ['mapped_by_rules', 'scored'] : []);
  step('the audit export carries the journey reason codes', want.every(r => reasons.has(r)), want.filter(r => !reasons.has(r)).join(',') || want.length + ' codes');

  // ---- no-JavaScript approval path (htmx and every script disabled): same session, fresh application
  const storage = await ctx.storageState();
  const njs = await browser.newContext({ ignoreHTTPSErrors: true, javaScriptEnabled: false, storageState: storage });
  const p2 = await njs.newPage();
  await p2.goto(base + '/postings/6');
  await p2.click('text=Shortlist and start an application'); await p2.waitForURL(/draft/);
  const holds = await cardHoldsStill(p2);
  step('draft card holds still under a pointer at its bottom edge (no :hover feedback loop)', holds.length === 1, holds.length + ' distinct position(s): ' + holds.slice(0, 4).join(' | '));
  await p2.click('text=Prepare template draft', { timeout: 10000 });
  await ackArea(p2); await p2.locator('#company').press('Enter');
  step('no-JavaScript: shortlist, prepare and dry-run approve complete', (await txt(p2, '[data-testid=app-state]')) === 'dry_run_recorded');
  await njs.close();

  await page.screenshot({ path: path.join(ev, 'audit-final.png'), fullPage: true });
  step('no uncaught page errors', errors.length === 0, errors.join(' | '));
  const expected = consoleBad.filter(m => /Failed to load resource: the server responded with a status of 40\d/.test(m));
  const other = consoleBad.filter(m => !expected.includes(m));
  step('no browser console errors besides the deliberate 4xx refusals', other.length === 0 && expected.length >= 8, 'refusals=' + expected.length + ' other=' + other.join(' | '));
  step('T089 canary: no synthetic overlay value in any textual HTTP response the browser received', canaryHits.length === 0 && bodiesChecked > 50, bodiesChecked + ' bodies checked, hits=' + canaryHits.join(','));
  fs.writeFileSync(path.join(ev, 'journey.log'), log.join('\n') + '\n');
  await browser.close();
  console.log(failed === 0 ? 'JOURNEY OK' : 'JOURNEY FAILED: ' + failed);
  process.exit(failed === 0 ? 0 : 1);

  // keyboard-only walk, focus, reduced motion, 200% zoom (WCAG 2.1.1, 2.1.2, 2.4.7, 2.4.11, 1.4.4, 1.4.10, 2.3.3)
  async function keyboardAndZoom() {
    const focusable = 'a[href], button:not([disabled]), input:not([type=hidden]):not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
    // 1. every interactive element of a page is reached by Tab in DOM order, shows a focus indicator, and focus leaves the page (no trap)
    for (const [name, url] of [['inbox', '/?provider=remotive'], ['draft', bypassURL.replace(base, '')], ['settings', '/settings'], ['tracking', '/applications/' + app1 + '/tracking'], ['queue', '/queue']]) {
      await page.goto(base + url);
      const expected = await page.evaluate(sel => [...document.querySelectorAll(sel)].filter(e => e.offsetParent !== null || e.getClientRects().length).map(e => (e.id || e.getAttribute('name') || e.textContent.trim().slice(0, 30) || e.tagName)), focusable);
      await page.evaluate(() => { document.activeElement && document.activeElement.blur(); window.scrollTo(0, 0); });
      const reached = [], noRing = [];
      for (let i = 0; i < expected.length + 3; i++) {
        await page.keyboard.press('Tab');
        const r = await page.evaluate(() => { const a = document.activeElement; if (!a || a === document.body) return null; const cs = getComputedStyle(a);
          return { id: a.id || a.getAttribute('name') || a.textContent.trim().slice(0, 30) || a.tagName, native: a.type === 'date', ring: (cs.outlineStyle !== 'none' && parseFloat(cs.outlineWidth) >= 2) || (cs.boxShadow !== 'none' && /(^|\s)[2-9]px/.test(cs.boxShadow.replace(/rgba?\([^)]*\)/g, ''))) }; });
        if (!r) break; if (reached[reached.length - 1] !== r.id) reached.push(r.id); if (!r.ring && !r.native) noRing.push(r.id);
      }
      const missed = expected.filter(x => !reached.includes(x));
      step('keyboard walk ' + name + ': all ' + expected.length + ' interactive elements reachable by Tab, focus leaves the page (no trap)', missed.length === 0 && reached.length <= expected.length + 2, 'missed=' + missed.join('|') + ' reached=' + reached.length);
      step('keyboard walk ' + name + ': every focused element shows a >= 2px focus indicator (outline or ring)', noRing.length === 0, noRing.join('|') || reached.length + ' checked');
    }
    // 2. skip link: the first Tab on a page targets "Skip to content", Enter moves focus to main
    await page.goto(base + '/settings'); await page.evaluate(() => document.activeElement && document.activeElement.blur());
    await page.keyboard.press('Tab');
    const skipOK = await page.evaluate(() => document.activeElement && document.activeElement.classList.contains('od-skip-link'));
    await page.keyboard.press('Enter');
    step('skip link is the first Tab stop and moves focus to the main region', skipOK && (await page.evaluate(() => document.activeElement && document.activeElement.id)) === 'main');
    // 3. a complete shortlist -> prepare -> dry-run approval with the keyboard alone (posting 2, never touched by the mouse)
    await page.goto(base + '/postings/2');
    const tabTo = async (pred, max) => { await page.waitForLoadState('load'); for (let i = 0; i < max; i++) { await page.keyboard.press('Tab'); if (await page.evaluate(pred)) return true; } return false; };
    await page.evaluate(() => document.activeElement && document.activeElement.blur());
    const enter = () => Promise.all([page.waitForNavigation({ timeout: 8000 }), page.keyboard.press('Enter')]);
    const a = await tabTo(() => document.activeElement && /Shortlist/.test(document.activeElement.textContent), 40); await enter();
    await page.evaluate(() => document.activeElement && document.activeElement.blur());
    const b = await tabTo(() => document.activeElement && /Prepare template draft/.test(document.activeElement.textContent), 40); await enter();
    await page.evaluate(() => document.activeElement && document.activeElement.blur());
    // JSK-T187b: a report that demands the area confirmation is acknowledged with the keyboard alone (Tab to the checkbox, Space)
    let kAck = true;
    if (await page.locator('[data-testid=area-confirm]').count()) {
      kAck = await tabTo(() => document.activeElement && document.activeElement.getAttribute('data-testid') === 'area-confirm', 60);
      await page.keyboard.press('Space');
      kAck = kAck && await page.locator('[data-testid=area-confirm]').isChecked();
    }
    step('keyboard only: the area-overlap confirmation can be ticked with Tab and Space (T187b)', kAck);
    const c = await tabTo(() => document.activeElement && document.activeElement.getAttribute('data-testid') === 'approve', 60); await enter();
    step('keyboard only: shortlist, prepare and dry-run approve complete', a && b && c && (await txt(page, '[data-testid=app-state]')) === 'dry_run_recorded', [a, b, c].join(','));
    // 4. reduced motion: no transition or animation runs
    await page.emulateMedia({ reducedMotion: 'reduce' });
    const motion = await page.evaluate(() => [...document.querySelectorAll('*')].filter(e => { const s = getComputedStyle(e); return parseFloat(s.transitionDuration) > 0.01 || (s.animationName !== 'none' && parseFloat(s.animationDuration) > 0.01); }).length);
    step('prefers-reduced-motion: no element transitions or animates', motion === 0, 'moving elements=' + motion);
    // 5. 200% zoom (1280 CSS px -> 640 CSS px at DPR 2) and 400% reflow (320 CSS px): no horizontal scroll, controls still reachable
    for (const [w, label] of [[640, '200%'], [320, '400% (reflow)']]) {
      await cdp.send('Emulation.setDeviceMetricsOverride', { width: w, height: 900, deviceScaleFactor: 1280 / w, mobile: false });
      for (const [name, url] of [['inbox', '/?provider=remotive'], ['draft', bypassURL.replace(base, '')], ['settings', '/settings'], ['audit', '/audit']]) {
        await page.goto(base + url);
        const m = await page.evaluate(() => ({ sw: document.documentElement.scrollWidth, iw: window.innerWidth }));
        const ok = m.sw <= m.iw + 1;
        step('zoom ' + label + ' ' + name + ': content reflows with no horizontal scroll', ok, 'scrollWidth=' + m.sw + ' innerWidth=' + m.iw);
        if (w === 640 && ['inbox', 'draft', 'settings'].includes(name)) await page.screenshot({ path: path.join(ev, 'zoom200-' + name + '.png'), fullPage: false });
      }
    }
    await cdp.send('Emulation.clearDeviceMetricsOverride');
    await page.emulateMedia({ reducedMotion: 'no-preference' });
  }
})().catch(e => { console.error('FATAL', e); process.exit(2); });
