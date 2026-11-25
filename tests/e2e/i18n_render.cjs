// i18n real-browser render check (FONT-001 / JSK-T157 / T126): real jobseekerd, real Chromium, virtual passkey. For each language in
// LANGS (Accept-Language negotiation) and each page it records <html lang/dir>, takes a screenshot, and asks Chromium (CDP
// CSS.getPlatformFontsForNode) WHICH font actually drew every text node. A glyph drawn by a non-custom (system) font means the
// shipped font set did not cover it (tofu risk) and is reported. A control injects Thai text (no shipped glyphs) and must be flagged.
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
const E = process.env, base = E.BASE_URL, ev = path.join(E.EVIDENCE_DIR, 'frames');
const langs = (E.LANGS || 'en').split(',');
fs.mkdirSync(ev, { recursive: true });
async function fontsUsed(cdp, page) {
  const { root } = await cdp.send('DOM.getDocument', { depth: -1 });
  const { nodeIds } = await cdp.send('DOM.querySelectorAll', { nodeId: root.nodeId, selector: 'body, body *' });
  const agg = {}; const sysChars = {};
  for (const nodeId of nodeIds) {
    let r; try { r = await cdp.send('CSS.getPlatformFontsForNode', { nodeId }); } catch (e) { continue; }
    for (const f of r.fonts) { const k = f.familyName + (f.isCustomFont ? ' [custom]' : ' [SYSTEM]'); agg[k] = (agg[k] || 0) + f.glyphCount; if (!f.isCustomFont) { try { const o = await cdp.send('DOM.resolveNode', { nodeId }); const t = await cdp.send('Runtime.callFunctionOn', { objectId: o.object.objectId, returnByValue: true, functionDeclaration: 'function(){return [...this.childNodes].filter(n=>n.nodeType===3).map(n=>n.nodeValue).join(" ")+" <"+this.tagName+"."+this.className+">"}' }); (sysChars[k] = sysChars[k] || new Set()).add(t.result.value.slice(0, 80)); } catch (e) {} } }
  }
  Object.defineProperty(agg, '_sys', { value: Object.fromEntries(Object.entries(sysChars).map(([k, v]) => [k, [...v].slice(0, 6)])), enumerable: false });
  return agg;
}
(async () => {
  const browser = await chromium.launch({ args: ['--host-resolver-rules=MAP js.lan 127.0.0.1', '--ignore-certificate-errors'] });
  const ctx = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 900 } });
  const page = await ctx.newPage();
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('WebAuthn.enable');
  await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
  await cdp.send('DOM.enable'); await cdp.send('CSS.enable');
  await page.goto(base + '/login', { waitUntil: 'networkidle' });
  await page.fill('#bootstrap-token', E.BOOTSTRAP_TOKEN);
  await page.click('#passkey-register');
  await page.waitForFunction(() => document.getElementById('passkey-status').textContent === 'ok', null, { timeout: 15000 });
  await page.click('#passkey-login');
  await page.waitForURL(base + '/', { timeout: 15000 });
  const pages = { inbox: '/', settings: '/settings', intake: '/intake', merges: '/queue/merges' };
  const manifest = [];
  for (const lang of langs) {
    await page.setExtraHTTPHeaders({ 'Accept-Language': lang });
    for (const scheme of ['light', 'dark']) {
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      for (const [name, p] of Object.entries(pages)) {
        await page.goto(base + p, { waitUntil: 'networkidle' });
        await page.evaluate(() => document.fonts.ready);
        const meta = await page.evaluate(() => ({ lang: document.documentElement.lang, dir: document.documentElement.dir, chars: [...new Set([...document.body.innerText])].filter(c => c.trim()).length, bodyDir: getComputedStyle(document.body).direction }));
        const fonts = await fontsUsed(cdp, page);
        const file = `${name}-${lang}-${scheme}.png`;
        await page.screenshot({ path: path.join(ev, file) });
        manifest.push({ file, page: name, lang, scheme, ...meta, fonts, sysText: fonts._sys });
      }
    }
  }
  // control: Thai has no glyph in any shipped font; the detector must report a SYSTEM font for it
  await page.goto(base + '/settings', { waitUntil: 'networkidle' });
  await page.evaluate(() => { const d = document.createElement('p'); d.id = 'thai-control'; d.textContent = 'สวัสดีครับ ทดสอบ'; document.body.prepend(d); });
  const ctl = await fontsUsed(cdp, page);
  const thai = Object.keys(ctl).filter(k => k.endsWith('[SYSTEM]'));
  fs.writeFileSync(path.join(E.EVIDENCE_DIR, 'control-thai.json'), JSON.stringify({ fonts: ctl, systemFonts: thai }, null, 1));
  fs.writeFileSync(path.join(E.EVIDENCE_DIR, 'manifest.json'), JSON.stringify(manifest, null, 1));
  await browser.close();
})().catch(e => { console.error(String(e)); process.exit(1); });
