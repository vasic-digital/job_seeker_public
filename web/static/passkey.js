// Passkey ceremonies for login, first-passkey bootstrap and step-up. The server verifies everything; this file only
// moves WebAuthn options and results between navigator.credentials and the JSON endpoints.
(function () {
  'use strict';
  var b64u = {
    dec: function (s) { s = s.replace(/-/g, '+').replace(/_/g, '/'); while (s.length % 4) s += '='; var r = atob(s), a = new Uint8Array(r.length); for (var i = 0; i < r.length; i++) a[i] = r.charCodeAt(i); return a.buffer; },
    enc: function (buf) { var a = new Uint8Array(buf), s = ''; for (var i = 0; i < a.length; i++) s += String.fromCharCode(a[i]); return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, ''); }
  };
  var meta = document.querySelector('meta[name="csrf-token"]');
  function status(msg) { var el = document.getElementById('passkey-status'); if (el) el.textContent = msg; }
  function post(url, headers, body) {
    var h = Object.assign({ 'Content-Type': 'application/json' }, headers || {});
    if (meta) h['X-CSRF-Token'] = meta.content;
    return fetch(url, { method: 'POST', credentials: 'same-origin', headers: h, body: body || '{}' });
  }
  function decodeCreate(o) {
    var p = o.publicKey; p.challenge = b64u.dec(p.challenge); p.user.id = b64u.dec(p.user.id);
    (p.excludeCredentials || []).forEach(function (c) { c.id = b64u.dec(c.id); }); return o;
  }
  function decodeGet(o) {
    var p = o.publicKey; p.challenge = b64u.dec(p.challenge);
    (p.allowCredentials || []).forEach(function (c) { c.id = b64u.dec(c.id); }); return o;
  }
  function encCreate(c) {
    return JSON.stringify({ id: c.id, rawId: b64u.enc(c.rawId), type: c.type, response: { attestationObject: b64u.enc(c.response.attestationObject), clientDataJSON: b64u.enc(c.response.clientDataJSON) } });
  }
  function encGet(c) {
    var r = c.response;
    return JSON.stringify({ id: c.id, rawId: b64u.enc(c.rawId), type: c.type, response: { authenticatorData: b64u.enc(r.authenticatorData), clientDataJSON: b64u.enc(r.clientDataJSON), signature: b64u.enc(r.signature), userHandle: r.userHandle ? b64u.enc(r.userHandle) : null } });
  }
  function assert(beginURL, finishURL) {
    return post(beginURL).then(function (r) { if (!r.ok) throw new Error('begin'); return r.json(); })
      .then(function (o) { return navigator.credentials.get(decodeGet(o)); })
      .then(function (c) { return post(finishURL, null, encGet(c)); })
      .then(function (r) { if (!r.ok) throw new Error('finish'); return r; });
  }
  var login = document.getElementById('passkey-login');
  if (login) login.addEventListener('click', function () {
    status(login.dataset.working || '');
    assert('/login/begin', '/login/finish').then(function () { location.assign(login.dataset.next || '/'); })
      .catch(function () { status(login.dataset.fail || 'failed'); });
  });
  var reg = document.getElementById('passkey-register');
  if (reg) reg.addEventListener('click', function () {
    var tok = document.getElementById('bootstrap-token').value;
    post('/login/register/begin', { 'X-Bootstrap-Token': tok }).then(function (r) { if (!r.ok) throw new Error('begin'); return r.json(); })
      .then(function (o) { return navigator.credentials.create(decodeCreate(o)); })
      .then(function (c) { return post('/login/register/finish?label=console', null, encCreate(c)); })
      .then(function (r) { if (!r.ok) throw new Error('finish'); status('ok'); })
      .catch(function () { status('failed'); });
  });
  var step = document.getElementById('stepup');
  if (step) step.addEventListener('click', function () {
    assert('/stepup/begin', '/stepup/finish').then(function () { status('ok'); }).catch(function () { status('failed'); });
  });
})();
