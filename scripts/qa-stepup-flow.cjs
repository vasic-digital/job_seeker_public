// qa-stepup-flow.cjs - the passkey STEP-UP flow of the real console, recorded as VIDEO plus per-step frames (JSK-T084/T111).
// usage: node qa-stepup-flow.cjs <base-url> <out-dir>   env: BOOTSTRAP_TOKEN, E2E_NODE_MODULES (dir holding playwright)
//
// Why a separate driver: the HelixQA web bridge keeps no WebAuthn authenticator alive between its per-action processes, so a
// passkey ceremony cannot run inside the autonomous bank. This driver uses the CDP VIRTUAL authenticator (an emulated device, the
// same class of evidence as scripts/qa-signin.cjs) and exercises a SAFE state change that is gated by step-up:
//   kill switch ON (never needs step-up) -> wait until the sign-in passkey check is older than five minutes (real clock, no hook)
//   -> turn it OFF: REFUSED step_up_required -> passkey step-up ceremony -> turn it OFF: accepted.
// Nothing is applied or sent; live mode is never turned on. Frames are 1920x937 PNGs for scripts/qa-stepup-verify.sh (vision
// verdicts through helixqa-verify-vision); the video is a window-scoped recording of this one browser page.
// stdout: one JSON document (steps with DOM facts and HTTP status codes). No cookie or token value is printed.
const fs = require('fs'), path = require('path');
const nm = process.env.E2E_NODE_MODULES, tok = process.env.BOOTSTRAP_TOKEN;
const [base, out] = process.argv.slice(2);
if (!nm || !tok || !base || !out) { console.error('usage: BOOTSTRAP_TOKEN=... E2E_NODE_MODULES=... qa-stepup-flow.cjs <base-url> <out-dir>'); process.exit(2); }
const { chromium } = require(path.join(nm, 'playwright'));
const AGE_MS = Number(process.env.STEPUP_AGE_MS || (5 * 60 + 8) * 1000); // StepUpTTL is 5 minutes (internal/auth/session.go)
const sleep = ms => new Promise(r => setTimeout(r, ms));
(async () => {
  fs.mkdirSync(path.join(out, 'frames'), { recursive: true });
  const b = await chromium.launch({ args: [] });
  const ctx = await b.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1920, height: 937 }, recordVideo: { dir: path.join(out, 'video-raw'), size: { width: 1280, height: 624 } } });
  const page = await ctx.newPage();
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('WebAuthn.enable');
  await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
  const steps = [];
  const txt = async s => ((await page.locator(s).first().textContent()) || '').trim();
  const frame = async (id) => { await page.waitForTimeout(400); const f = path.join('frames', id + '.png'); await page.screenshot({ path: path.join(out, f) }); return f; };
  const rec = (id, o) => { steps.push(Object.assign({ id, t: new Date().toISOString() }, o)); };
  let status = null;
  page.on('response', r => { if (r.request().method() === 'POST' && /\/settings\/kill-switch$/.test(new URL(r.url()).pathname)) status = r.status(); });

  await page.goto(base + '/login');
  await page.fill('#bootstrap-token', tok);
  await page.click('#passkey-register');
  await page.waitForFunction(() => document.getElementById('passkey-status').textContent === 'ok', null, { timeout: 15000 });
  await page.click('#passkey-login');
  await page.waitForURL(base + '/', { timeout: 15000 });
  const t0 = Date.now();

  await page.goto(base + '/settings');
  rec('01-settings-initial', { frame: await frame('01-settings-initial'), kill: await txt('[data-testid=kill-state]'), live: await txt('[data-testid=live-state]') });

  status = null;
  await page.click('[data-testid=kill-toggle]');
  await page.waitForURL(base + '/settings');
  rec('02-kill-on', { frame: await frame('02-kill-on'), http: status, kill: await txt('[data-testid=kill-state]'), step_up_needed: false });

  const wait = AGE_MS - (Date.now() - t0);
  if (wait > 0) await sleep(wait);
  rec('waited', { ms_since_signin: Date.now() - t0 });

  status = null;
  await page.click('[data-testid=kill-toggle]');
  await page.waitForLoadState('load');
  const refused = await page.locator('[data-error-code]').first().getAttribute('data-error-code').catch(() => null);
  rec('03-off-refused', { frame: await frame('03-off-refused'), http: status, error_code: refused, kill: await txt('[data-testid=kill-state]') });

  await page.click('#stepup');
  await page.waitForFunction(() => document.getElementById('passkey-status').textContent === 'ok', null, { timeout: 15000 }).catch(() => {});
  rec('04-stepup', { frame: await frame('04-stepup'), passkey_status: await txt('#passkey-status') });

  status = null;
  await page.click('[data-testid=kill-toggle]');
  await page.waitForURL(base + '/settings');
  rec('05-kill-off', { frame: await frame('05-kill-off'), http: status, kill: await txt('[data-testid=kill-state]'), live: await txt('[data-testid=live-state]') });

  const v = page.video();
  await ctx.close();
  const vp = await v.path();
  const dst = path.join(out, 'stepup-flow.webm');
  fs.renameSync(vp, dst);
  fs.rmSync(path.join(out, 'video-raw'), { recursive: true, force: true });
  await b.close();
  console.log(JSON.stringify({ steps, video: 'stepup-flow.webm', video_bytes: fs.statSync(dst).size }, null, 2));
})().catch(e => { console.error(String((e && e.message) || e)); process.exit(1); });
