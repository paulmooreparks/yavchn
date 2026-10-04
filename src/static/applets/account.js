/* Account content uses PUDL's applet lifecycle in a window or Classic page.
   Passkeys and the picture's cropping follow parkscomputing.com's
   accounts.js and account.js. */
(function () {
  'use strict';

  /* === Passkeys =========================================================
     WebAuthn's JSON methods where the browser has them, and the same
     conversion by hand where it does not. */
  function toBytes(b64url) {
    var s = b64url.replace(/-/g, '+').replace(/_/g, '/');
    while (s.length % 4) s += '=';
    var bin = atob(s), out = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out.buffer;
  }
  function toB64url(buf) {
    var bytes = new Uint8Array(buf), bin = '';
    for (var i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
    return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  }
  function unwrap(o) { return o && o.publicKey ? o.publicKey : o; }
  function creationOptions(o) {
    o = unwrap(o);
    if (PublicKeyCredential.parseCreationOptionsFromJSON) return PublicKeyCredential.parseCreationOptionsFromJSON(o);
    var c = Object.assign({}, o);
    c.challenge = toBytes(o.challenge);
    c.user = Object.assign({}, o.user, { id: toBytes(o.user.id) });
    c.excludeCredentials = (o.excludeCredentials || []).map(function (x) { return Object.assign({}, x, { id: toBytes(x.id) }); });
    return c;
  }
  function requestOptions(o) {
    o = unwrap(o);
    if (PublicKeyCredential.parseRequestOptionsFromJSON) return PublicKeyCredential.parseRequestOptionsFromJSON(o);
    var r = Object.assign({}, o);
    r.challenge = toBytes(o.challenge);
    r.allowCredentials = (o.allowCredentials || []).map(function (x) { return Object.assign({}, x, { id: toBytes(x.id) }); });
    return r;
  }
  function credentialJSON(cred) {
    if (typeof cred.toJSON === 'function') return cred.toJSON();
    var r = cred.response, out = {
      id: cred.id, rawId: toB64url(cred.rawId), type: cred.type,
      authenticatorAttachment: cred.authenticatorAttachment || null,
      clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {},
      response: { clientDataJSON: toB64url(r.clientDataJSON) }
    };
    if (r.attestationObject) {
      out.response.attestationObject = toB64url(r.attestationObject);
      if (r.getTransports) out.response.transports = r.getTransports();
    } else {
      out.response.authenticatorData = toB64url(r.authenticatorData);
      out.response.signature = toB64url(r.signature);
      out.response.userHandle = r.userHandle ? toB64url(r.userHandle) : null;
    }
    return out;
  }

  window.pudlApplets.register('account', { init: function (root, opts) {
    window.yavchnAccountUI(root);
    function returnTo() { return opts.host === 'window' ? location.pathname + location.search : '/account?view=classic'; }
    function token() { var input = root.querySelector('input[name="csrf"]'); return input ? input.value : ''; }
    function returning(event) {
      var input = event.target.querySelector('input[name="return_to"]');
      if (input) input.value = returnTo();
    }
    root.addEventListener('submit', returning);

    async function post(url, body) {
      var r = await fetch(url, {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json', 'X-CSRF-Token': token() },
        body: JSON.stringify(body || {})
      });
      var value = null;
      try { value = await r.json(); } catch (e) {}
      if (!r.ok) throw new Error((value && value.error) || 'That did not work. Reload the page and try again.');
      return value;
    }
    /* One ceremony: 'register' adds a passkey to this account; 'signin'
       signs in with one, which needs no address. */
    async function ceremony(action) {
      if (!window.PublicKeyCredential || !navigator.credentials) throw new Error('This browser does not support passkeys. Use another way to sign in.');
      var adding = action === 'register';
      var options = await post(adding ? '/account/passkeys/options' : '/auth/passkey/options', { return_to: returnTo() });
      var credential = adding ? await navigator.credentials.create({ publicKey: creationOptions(options) })
        : await navigator.credentials.get({ publicKey: requestOptions(options) });
      var name = root.querySelector('[data-passkey-name]');
      var done = await post(adding ? '/account/passkeys' : '/auth/passkey',
        { credential: credentialJSON(credential), name: name ? name.value : '' });
      location.assign(done.redirect);
    }
    if (window.PublicKeyCredential) root.querySelectorAll('[data-passkey-ui]').forEach(function (el) { el.hidden = false; });
    async function press(button) {
      var status = root.querySelector('[data-passkey-status]');
      button.disabled = true;
      if (status) { status.textContent = ''; status.hidden = true; }
      try { await ceremony(button.dataset.passkey); } catch (error) {
        if (status) {
          status.hidden = false;
          status.textContent = error && (error.name === 'NotAllowedError' || error.name === 'AbortError')
            ? 'The passkey request was cancelled or timed out. You can try again.'
            : error && error.name === 'InvalidStateError'
              ? 'This device already holds a passkey for your account. Use a different device or app for the next one.'
              : (error && error.message) || 'That did not work. Try again.';
        }
      } finally { button.disabled = false; }
    }
    function clicked(event) {
      var button = event.target.closest && event.target.closest('[data-passkey]');
      if (button && root.contains(button)) { event.preventDefault(); press(button); }
    }
    root.addEventListener('click', clicked);

    /* === The picture ====================================================
       A chosen picture, cropped to its centre square and drawn at no more
       than 256 pixels, as WebP where the browser can write it and PNG
       otherwise. Drawing it again leaves its metadata, such as where a
       photo was taken, behind. Without script the file goes as it is. */
    async function square(file) {
      var bitmap = await createImageBitmap(file);
      var side = Math.min(bitmap.width, bitmap.height), size = Math.min(256, side);
      var canvas = document.createElement('canvas');
      canvas.width = canvas.height = size;
      canvas.getContext('2d').drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, size, size);
      if (bitmap.close) bitmap.close();
      return new Promise(function (resolve, reject) {
        canvas.toBlob(function (blob) { if (blob) resolve(blob); else reject(new Error('could not draw the picture')); }, 'image/webp', 0.88);
      });
    }
    function picture(event) {
      var form = event.target;
      if (!form.matches('[data-account-picture-form]') || !window.createImageBitmap) return;
      var file = form.querySelector('input[type="file"]').files[0];
      if (!file) return;
      event.preventDefault();
      returning(event);
      square(file).then(function (blob) {
        var body = new FormData(form);
        body.set('picture', blob, blob.type === 'image/webp' ? 'picture.webp' : 'picture.png');
        return fetch(form.action, { method: 'POST', body: body, credentials: 'same-origin' });
      }).then(function (r) {
        if (!r.ok) throw new Error('upload failed');
        location.assign(r.url);
      }).catch(function () { form.submit(); });
    }
    root.addEventListener('submit', picture);

    return {
      menus: function () {
        var items = [];
        var login = root.querySelector('form[action="/auth/github"]');
        var exported = root.querySelector('a[href="/account/export"]');
        var imported = root.querySelector('[data-account-import]');
        var logout = root.querySelector('form[action="/account/session"]');
        var revoke = root.querySelector('form[action="/account/sessions"]');
        if (login) items.push({ label: login.querySelector('button').textContent, run: function () { login.requestSubmit(); } });
        if (imported && !imported.hidden) items.push({ label: "Import this browser's anonymous data", run: function () { imported.click(); } });
        if (exported) items.push({ label: 'Export account data', run: function () { exported.click(); } });
        if (revoke) items.push({ label: 'Sign out other devices', run: function () { revoke.requestSubmit(); } });
        if (logout) items.push({ label: 'Sign out this browser', run: function () { logout.requestSubmit(); } });
        var titles = [{ label: 'Account', items: window.yavchn.identityMenu(root, 'Copy account link', '/account?view=classic') }];
        if (items.length) titles.push({ label: 'Manage', items: items });
        return { titles: titles };
      },
      destroy: function () {
        root.removeEventListener('submit', returning);
        root.removeEventListener('submit', picture);
        root.removeEventListener('click', clicked);
      }
    };
  } });
})();
