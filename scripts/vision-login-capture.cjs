// vision-login-capture.cjs - perform the REAL passkey sign-in against a running jobseekerd with a CDP virtual authenticator
// (the same technique as tests/e2e/journey.cjs), then screenshot authenticated pages (JSK-T084/T111 vision frames).
// usage: JSK_PLAYWRIGHT_NODE_MODULES=... BOOTSTRAP_TOKEN=... node vision-login-capture.cjs <base-url> <outdir>
// Writes <outdir>/live-auth-inbox.png and live-auth-settings.png, prints one JSON line. Exit 0 ok, 1 ceremony failed, 2 usage.
// Limits: the virtual authenticator is software (no hardware key); the DB is empty (no seeded postings) so the inbox frame shows
// the empty state, not rows; Chromium ignores SSL_CERT_FILE so cert errors are ignored for the screenshot only (the CA chain is
// proved by `helixqa http`).
const path = require('path');
const nm = process.env.JSK_PLAYWRIGHT_NODE_MODULES, tok = process.env.BOOTSTRAP_TOKEN;
if (!nm || !tok) { console.error('set JSK_PLAYWRIGHT_NODE_MODULES and BOOTSTRAP_TOKEN'); process.exit(2); }
const { chromium } = require(path.join(nm, 'playwright'));
const [base, out] = process.argv.slice(2);
if (!base || !out) { console.error('usage: vision-login-capture.cjs <base-url> <outdir>'); process.exit(2); }
(async () => {
  const b = await chromium.launch({ args: ['--ignore-certificate-errors'], ...(process.env.JSK_CHROMIUM_EXECUTABLE ? { executablePath: process.env.JSK_CHROMIUM_EXECUTABLE } : {}) });
  const ctx = await b.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 900 }, colorScheme: 'light' });
  const page = await ctx.newPage();
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('WebAuthn.enable');
  await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
  await page.goto(base + '/login', { waitUntil: 'networkidle', timeout: 30000 });
  await page.fill('#bootstrap-token', tok);
  await page.click('#passkey-register');
  await page.waitForFunction(() => document.getElementById('passkey-status').textContent === 'ok', null, { timeout: 15000 });
  await page.click('#passkey-login');
  await page.waitForURL(base + '/', { timeout: 15000 });
  const title = await page.title();
  if (!title.startsWith('Inbox')) { console.error('sign-in did not reach the inbox: ' + title); process.exit(1); }
  await page.screenshot({ path: path.join(out, 'live-auth-inbox.png') });
  await page.goto(base + '/settings', { waitUntil: 'networkidle', timeout: 30000 });
  await page.screenshot({ path: path.join(out, 'live-auth-settings.png') });
  console.log(JSON.stringify({ base, inbox_title: title, settings_title: await page.title() }));
  await b.close();
})().catch(e => { console.error(String(e)); process.exit(1); });
