// qa-signin.cjs - sign in to a real jobseekerd with a CDP VIRTUAL authenticator and write the session cookie to a
// 0600 JSON file in the shape HelixQA's -web-cookie-file takes (JSK-T084/T111, umbrella 11.4.278).
// usage: node qa-signin.cjs <base-url> <out-cookie-file>      env: BOOTSTRAP_TOKEN, E2E_NODE_MODULES (dir holding playwright)
// The cookie VALUE is never printed or logged; stdout carries only the cookie name, flags and the file mode.
const fs = require('fs'), path = require('path');
const nm = process.env.E2E_NODE_MODULES;
if (!nm) { console.error('set E2E_NODE_MODULES to a node_modules directory containing playwright'); process.exit(2); }
const { chromium } = require(path.join(nm, 'playwright'));
const [base, out] = process.argv.slice(2), tok = process.env.BOOTSTRAP_TOKEN;
if (!base || !out || !tok) { console.error('usage: BOOTSTRAP_TOKEN=... qa-signin.cjs <base-url> <out-cookie-file>'); process.exit(2); }
(async () => {
  const b = await chromium.launch({ args: [] });
  const ctx = await b.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 900 } });
  const page = await ctx.newPage();
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('WebAuthn.enable');
  await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
  await page.goto(base + '/login');
  await page.fill('#bootstrap-token', tok);
  await page.click('#passkey-register');
  await page.waitForFunction(() => document.getElementById('passkey-status').textContent === 'ok', null, { timeout: 15000 });
  await page.click('#passkey-login');
  await page.waitForURL(base + '/', { timeout: 15000 });
  const c = (await ctx.cookies(base)).filter(x => x.name === '__Host-js_session');
  if (c.length !== 1) { console.error('expected exactly one __Host-js_session cookie, got ' + c.length); process.exit(1); }
  const x = c[0];
  fs.writeFileSync(out, JSON.stringify([{ name: x.name, value: x.value, path: '/', httpOnly: x.httpOnly, secure: x.secure, sameSite: x.sameSite }]), { mode: 0o600 });
  fs.chmodSync(out, 0o600);
  console.log(JSON.stringify({ cookie: x.name, httpOnly: x.httpOnly, secure: x.secure, sameSite: x.sameSite, mode: (fs.statSync(out).mode & 0o777).toString(8) }));
  await b.close();
})().catch(e => { console.error(String(e && e.message || e)); process.exit(1); });
