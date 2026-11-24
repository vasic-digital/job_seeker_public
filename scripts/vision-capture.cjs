// vision-capture.cjs - take ONE screenshot of a URL with real Chromium (JSK-010 vision QA frames).
// usage: node vision-capture.cjs <url> <out.png> [width] [height] [colorScheme]
// Playwright is resolved from JSK_PLAYWRIGHT_NODE_MODULES (a node_modules directory containing `playwright`).
// TLS: the page is served over the private CA; Chromium does not read SSL_CERT_FILE, so this capture step ignores
// certificate errors. That is acceptable for a SCREENSHOT only: the TLS trust chain is proved by the `helixqa http`
// leg (tests/live TestHelixQAHTTPBankAgainstRealDaemon), which trusts only the private CA.
const path = require('path');
const nm = process.env.JSK_PLAYWRIGHT_NODE_MODULES;
if (!nm) { console.error('set JSK_PLAYWRIGHT_NODE_MODULES to a node_modules directory containing playwright'); process.exit(2); }
const { chromium } = require(path.join(nm, 'playwright'));
const [url, out, w = '1280', h = '900', scheme = 'light'] = process.argv.slice(2);
if (!url || !out) { console.error('usage: vision-capture.cjs <url> <out.png> [w] [h] [scheme]'); process.exit(2); }
(async () => {
  const b = await chromium.launch({ args: ['--ignore-certificate-errors'] });
  const ctx = await b.newContext({ ignoreHTTPSErrors: true, viewport: { width: +w, height: +h }, colorScheme: scheme });
  const p = await ctx.newPage();
  const resp = await p.goto(url, { waitUntil: 'networkidle', timeout: 30000 });
  await p.screenshot({ path: out, fullPage: false });
  console.log(JSON.stringify({ url, status: resp ? resp.status() : null, title: await p.title(), out }));
  await b.close();
})().catch(e => { console.error(String(e)); process.exit(1); });
