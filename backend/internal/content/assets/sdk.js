/* sdk.js — the shim that runs inside every artifact.
 *
 * An artifact lives in an iframe with sandbox="allow-scripts" and WITHOUT
 * allow-same-origin, so in an opaque origin: localStorage, cookies and
 * window.parent throw in its face. The only way out is postMessage, and this
 * file is the bridge.
 *
 * It does three things:
 *
 *   1. Replaces localStorage with a proxy that looks synchronous but talks to
 *      the host. Data stays in the viewer's browser, never on the server, and
 *      separate per artifact: a compromised artifact can ruin its own data and
 *      nothing else.
 *
 *   2. Exposes window.Falconia (and window.SFA, its name before the project
 *      was renamed) with hooks for state versioning, so an artifact at version
 *      3 can read data saved by version 1.
 *
 *   3. Runs the iframe side of the review loop: when the host asks for
 *      comment mode, this file catches the click, describes the element hit
 *      and sends it back. The host cannot do it itself because it cannot see
 *      the artifact's DOM, and rightly so.
 *
 * ES2020, no dependencies, must work before the DOM exists.
 */
(function () {
  'use strict';

  const HANDSHAKE = '__SFA_HANDSHAKE__';
  const READY = '__SFA_READY__';

  let port = null;
  let hydrated = false;
  let readOnly = false;
  let declaredSchemaVersion = 1;
  let readyCb = null;
  const cache = new Map();
  const pending = [];
  const migrators = new Map();

  // ---------- Storage ----------

  const fakeStorage = {
    get length() { return cache.size; },
    key(i) { return Array.from(cache.keys())[i] ?? null; },
    getItem(k) { const v = cache.get(String(k)); return v === undefined ? null : v; },
    setItem(k, v) {
      const key = String(k), val = String(v);
      cache.set(key, val);
      if (readOnly) return;
      send({ type: 'storage:set', key, value: val });
    },
    removeItem(k) {
      const key = String(k);
      cache.delete(key);
      if (readOnly) return;
      send({ type: 'storage:del', key });
    },
    clear() {
      cache.clear();
      if (readOnly) return;
      send({ type: 'storage:clear' });
    },
  };

  function send(msg) {
    if (port) port.postMessage(msg);
    else pending.push(msg);
  }

  // In an opaque origin merely ACCESSING window.localStorage throws, before the
  // artifact's code gets a chance to handle it: the property must be
  // redefined, not assigned.
  function installStorageProxy() {
    for (const name of ['localStorage', 'sessionStorage']) {
      try {
        Object.defineProperty(window, name, {
          configurable: true, enumerable: true, get: () => fakeStorage,
        });
      } catch (_) { /* already redefined or not redefinable: never mind */ }
    }
  }

  // ---------- Public API ----------

  const SFA = {
    setSchemaVersion(n) { declaredSchemaVersion = Math.max(1, Number(n) || 1); },
    registerMigration(from, fn) { migrators.set(Number(from), fn); },
    onReady(cb) { readyCb = cb; if (hydrated) invokeReady(); },
    isReady() { return hydrated; },
    isReadOnly() { return readOnly; },
    save(state) {
      return new Promise((resolve) => {
        fakeStorage.setItem('__sfa_state__', JSON.stringify({ v: declaredSchemaVersion, s: state }));
        if (!readOnly) send({ type: 'artifact:state', schemaVersion: declaredSchemaVersion, state });
        resolve();
      });
    },
    load() {
      const raw = fakeStorage.getItem('__sfa_state__');
      if (!raw) return null;
      try { return JSON.parse(raw).s ?? null; } catch { return null; }
    },
    resize(height) { send({ type: 'ui:resize', height: Number(height) || 0 }); },
  };
  // Falconia is the name to use; SFA keeps artifacts written before the
  // rename working.
  Object.defineProperty(window, 'Falconia', { value: SFA, writable: false, configurable: false });
  Object.defineProperty(window, 'SFA', { value: SFA, writable: false, configurable: false });

  function runMigrations(state, fromVersion) {
    let v = Number(fromVersion) || 1;
    while (v < declaredSchemaVersion) {
      const step = migrators.get(v);
      // Without a migrator for this step, stop: applying the next one would
      // skip a transformation and hand the artifact data it believes it
      // understands and does not.
      if (!step) break;
      state = step(state) ?? state;
      v += 1;
    }
    return { state, version: v };
  }

  function invokeReady() {
    if (!readyCb) return;
    let state = null, version = declaredSchemaVersion;
    const raw = fakeStorage.getItem('__sfa_state__');
    if (raw) {
      try { const p = JSON.parse(raw); state = p.s ?? null; version = p.v ?? 1; } catch (_) {}
    }
    if (state !== null && version < declaredSchemaVersion) {
      const m = runMigrations(state, version);
      state = m.state; version = m.version;
      fakeStorage.setItem('__sfa_state__', JSON.stringify({ v: version, s: state }));
    }
    try { readyCb({ state, version, save: SFA.save, migrate: runMigrations }); }
    catch (e) { console.error('Falconia.onReady threw:', e); }
  }

  // ---------- Anchored comments ----------

  let commentMode = false;
  let anchors = [];
  let layer = null;

  // cssPathOf describes an element so it can be found again after someone —
  // usually another agent — regenerated the page.
  //
  // An id, if present and unique, wins over everything: it is the one thing a
  // markup rewrite tends to keep. Otherwise climb the tree with tag and
  // nth-of-type, stopping at eight levels, because a longer selector describes
  // yesterday's structure more than today's element.
  function cssPathOf(el) {
    if (!el || el.nodeType !== 1) return '';
    const parts = [];
    let node = el;
    for (let depth = 0; node && node.nodeType === 1 && depth < 8; depth++) {
      if (node.id && document.querySelectorAll('#' + CSS.escape(node.id)).length === 1) {
        parts.unshift('#' + CSS.escape(node.id));
        break;
      }
      const tag = node.tagName.toLowerCase();
      if (tag === 'body' || tag === 'html') { parts.unshift(tag); break; }
      const parent = node.parentElement;
      if (!parent) { parts.unshift(tag); break; }
      const sameTag = Array.from(parent.children).filter((c) => c.tagName === node.tagName);
      parts.unshift(sameTag.length > 1 ? `${tag}:nth-of-type(${sameTag.indexOf(node) + 1})` : tag);
      node = parent;
    }
    return parts.join(' > ');
  }

  function describe(el, ev) {
    const rect = el.getBoundingClientRect();
    const docH = Math.max(document.documentElement.scrollHeight, 1);
    const docW = Math.max(document.documentElement.scrollWidth, 1);
    return {
      selector: cssPathOf(el),
      quote: (el.textContent || '').trim().slice(0, 180),
      x: +(((ev ? ev.pageX : rect.left + window.scrollX)) / docW).toFixed(4),
      y: +(((ev ? ev.pageY : rect.top + window.scrollY)) / docH).toFixed(4),
    };
  }

  // resolveAnchor tries the clues from the most precise to the coarsest. It
  // returns null when none holds: the thread becomes detached and stays in the
  // side panel instead of being thrown away.
  function resolveAnchor(a) {
    if (!a) return null;
    if (a.selector) {
      try { const el = document.querySelector(a.selector); if (el) return el; } catch (_) {}
    }
    if (a.quote && a.quote.length > 8) {
      const needle = a.quote.slice(0, 60);
      const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_ELEMENT);
      let best = null;
      while (walker.nextNode()) {
        const el = walker.currentNode;
        if (el.children.length === 0 && (el.textContent || '').includes(needle)) { best = el; break; }
      }
      if (best) return best;
    }
    return null;
  }

  function ensureLayer() {
    if (layer && layer.isConnected) return layer;
    layer = document.createElement('div');
    layer.id = '__sfa_pins__';
    layer.setAttribute('style', [
      'position:absolute', 'inset:0', 'pointer-events:none',
      'z-index:2147483000', 'contain:layout style',
    ].join(';'));
    document.body.appendChild(layer);
    return layer;
  }

  function renderPins() {
    if (!document.body) return;
    const host = ensureLayer();
    host.textContent = '';
    const orphans = [];
    const docH = Math.max(document.documentElement.scrollHeight, 1);
    const docW = Math.max(document.documentElement.scrollWidth, 1);

    anchors.forEach((a) => {
      let top, left;
      const el = resolveAnchor(a.anchor);
      if (el) {
        const r = el.getBoundingClientRect();
        top = r.top + window.scrollY;
        left = r.right + window.scrollX;
      } else if (a.anchor && (a.anchor.x || a.anchor.y)) {
        top = (a.anchor.y || 0) * docH;
        left = (a.anchor.x || 0) * docW;
        orphans.push(a.id);
      } else {
        orphans.push(a.id);
        return;
      }
      const pin = document.createElement('button');
      pin.type = 'button';
      pin.textContent = String(a.n || '•');
      pin.title = a.title || 'Comment';
      pin.setAttribute('style', [
        'position:absolute', `top:${Math.max(0, top - 10)}px`, `left:${Math.max(0, left - 10)}px`,
        'pointer-events:auto', 'width:24px', 'height:24px', 'border-radius:999px',
        'border:2px solid #fff', 'cursor:pointer', 'font:600 12px/1 system-ui,sans-serif',
        'color:#fff', 'display:flex', 'align-items:center', 'justify-content:center',
        'box-shadow:0 2px 8px rgba(0,0,0,.35)',
        `background:${a.status === 'resolved' ? '#16a34a' : '#f59e0b'}`,
      ].join(';'));
      pin.addEventListener('click', (ev) => {
        ev.preventDefault(); ev.stopPropagation();
        send({ type: 'comment:focus', id: a.id });
      });
      host.appendChild(pin);
    });
    send({ type: 'comment:anchored', orphans });
  }

  // In comment mode the click is caught in the capture phase and stopped
  // there: otherwise it would follow the link or submit the form under the
  // spot the person only meant to point at.
  function onCommentClick(ev) {
    if (!commentMode) return;
    const el = ev.target;
    if (!el || (layer && layer.contains(el))) return;
    ev.preventDefault();
    ev.stopPropagation();
    setCommentMode(false);
    send({ type: 'comment:pick', anchor: describe(el, ev) });
  }

  function setCommentMode(on) {
    commentMode = !!on;
    if (document.documentElement) {
      document.documentElement.style.cursor = commentMode ? 'crosshair' : '';
    }
  }

  document.addEventListener('click', onCommentClick, true);
  window.addEventListener('resize', () => { if (anchors.length) renderPins(); });

  // ---------- Handshake ----------
  //
  // The host sends a single message carrying the port. From then on all
  // talk happens on it and the global listener is removed: another frame
  // trying to pass itself off as the host finds nobody listening.
  window.addEventListener('message', function init(ev) {
    const data = ev.data;
    if (!data || data.type !== HANDSHAKE || !ev.ports || !ev.ports[0]) return;
    port = ev.ports[0];
    readOnly = !!data.readOnly;
    port.onmessage = onHostMessage;
    if (readOnly) pending.length = 0;
    else while (pending.length) port.postMessage(pending.shift());
    port.postMessage({ type: 'storage:hydrate' });
    window.removeEventListener('message', init);
  });

  function onHostMessage(ev) {
    const msg = ev.data || {};
    switch (msg.type) {
      case 'storage:snapshot':
        cache.clear();
        for (const [k, v] of Object.entries(msg.entries || {})) cache.set(k, v);
        hydrated = true;
        port.postMessage({ type: READY, schemaVersion: declaredSchemaVersion });
        invokeReady();
        break;
      case 'comment:mode':
        setCommentMode(msg.on);
        break;
      case 'comment:anchors':
        anchors = Array.isArray(msg.anchors) ? msg.anchors : [];
        if (document.readyState === 'loading') {
          document.addEventListener('DOMContentLoaded', renderPins, { once: true });
        } else {
          renderPins();
        }
        break;
      case 'storage:ack':
        break;
    }
  }

  installStorageProxy();
})();
