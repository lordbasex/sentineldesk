/*
 * SentinelDesk
 * A collaborative operating system for people and AI agents.
 *
 * Copyright 2026 Federico Pereira <fpereira@cnsoluciones.com>
 * Co-authored by Nicolas Pereira <npereira@cnsoluciones.com>
 *
 * Licensed under the Apache License, Version 2.0.
 *
 * This product's name and logo are trademarks of Federico Pereira and are not
 * covered by the license above. See the README for the trademark policy.
 *
 * SPDX-License-Identifier: Apache-2.0
 */


/* Translations for the project site.
 *
 * The same mechanism the desktop's own documentation uses, adapted to a static
 * host. English is always the default and always the fallback: a missing key
 * shows the English string rather than a raw identifier, so a half-finished
 * translation degrades into a readable mix instead of gibberish.
 *
 * Static text is marked up in the HTML:
 *   data-i18n="key"              replaces the element's text
 *   data-i18n-html="key"         same, but the value may contain markup
 *   data-i18n-title="key"        replaces the title attribute
 *   data-i18n-alt="key"          replaces the alt attribute
 *
 * Three differences from the in-container version, all forced by this being a
 * static site served from a path prefix:
 *
 *   1. Paths are RELATIVE ('lang/en.json'). GitHub Pages serves the repository
 *      under /sentineldesk/, so an absolute '/lang/en.json' would 404.
 *   2. The language list is declared here rather than enumerated by a server,
 *      because there is no server to ask.
 *   3. ?lang= wins over the remembered choice and is written back into the URL
 *      on every switch. A shared link has to arrive in the language it was
 *      shared in, whatever the recipient picked last time on another page.
 *
 * The HTML ships with English already in it. That is deliberate: the page is
 * readable before this file loads, and if it never loads — blocked script,
 * network cut halfway — a visitor still gets the whole site rather than a
 * skeleton of empty elements.
 */

const STORAGE_KEY = 'sentineldesk_lang';
const FALLBACK = 'en';

const AVAILABLE = [
  { code: 'en', name: 'English',   label: 'EN' },
  { code: 'es', name: 'Español',   label: 'ES' },
  { code: 'pt', name: 'Português', label: 'PT' },
];

let dict = {};      // active language
let fallback = {};  // English, always loaded
let current = FALLBACK;

/** t returns a translated string, filling {placeholders} from vars. */
export function t(key, vars) {
  let s = dict[key] ?? fallback[key] ?? key;
  if (vars) {
    for (const [k, v] of Object.entries(vars)) s = s.split('{' + k + '}').join(v);
  }
  return s;
}

export function languages() { return AVAILABLE; }
export function currentLanguage() { return current; }

async function loadDict(code) {
  const res = await fetch('lang/' + encodeURIComponent(code) + '.json');
  if (!res.ok) throw new Error('missing translation: ' + code);
  return res.json();
}

/** apply walks the document and fills in every marked-up element. */
export function apply(root = document) {
  root.querySelectorAll('[data-i18n]').forEach((el) => {
    el.textContent = t(el.dataset.i18n);
  });
  root.querySelectorAll('[data-i18n-html]').forEach((el) => {
    el.innerHTML = t(el.dataset.i18nHtml);
  });
  root.querySelectorAll('[data-i18n-title]').forEach((el) => {
    el.title = t(el.dataset.i18nTitle);
  });
  root.querySelectorAll('[data-i18n-alt]').forEach((el) => {
    el.alt = t(el.dataset.i18nAlt);
  });

  // The two things outside the body that still have to follow the language:
  // what a browser tab says, and what a screen reader announces the page as.
  const title = document.querySelector('meta[name="page-key"]')?.content;
  if (title) document.title = t(title);
  document.documentElement.lang = current;
}

/** Marks the active button in the language switcher. */
function paintSwitcher() {
  // The group's own label follows the language too: a screen reader announcing
  // "Language" on a page reading Portuguese is a seam. Done here rather than at
  // mount time so it is refreshed on every switch, not just the first render.
  document.querySelectorAll('[data-lang-switcher]').forEach((host) => {
    host.setAttribute('aria-label', t('lang.aria'));
  });
  document.querySelectorAll('[data-lang]').forEach((el) => {
    const on = el.dataset.lang === current;
    el.classList.toggle('on', on);
    el.setAttribute('aria-pressed', String(on));
  });
}

/**
 * Rewrites ?lang= in the address bar without adding a history entry.
 *
 * replaceState rather than pushState on purpose: switching language is not a
 * navigation, and stacking it into history would turn Back into "undo my last
 * language click" instead of "go to the previous page".
 */
function stampURL() {
  try {
    const u = new URL(window.location.href);
    if (current === FALLBACK) u.searchParams.delete('lang');
    else u.searchParams.set('lang', current);
    history.replaceState(null, '', u);
  } catch (_) { /* file:// and other opaque origins */ }
}

/**
 * setLanguage switches language, remembers the choice, and re-renders.
 * Returns false when the language could not be loaded, leaving the previous
 * one in place — a failed switch must not blank the page.
 */
export async function setLanguage(code) {
  if (code === current) return true;
  if (!AVAILABLE.some((l) => l.code === code)) return false;
  try {
    dict = code === FALLBACK ? fallback : await loadDict(code);
  } catch (err) {
    console.warn('i18n:', err.message);
    return false;
  }
  current = code;
  try { localStorage.setItem(STORAGE_KEY, code); } catch (_) { /* private mode */ }
  apply();
  paintSwitcher();
  stampURL();
  return true;
}

/** Builds the EN · ES · PT switcher into every element marked for it. */
function mountSwitcher() {
  document.querySelectorAll('[data-lang-switcher]').forEach((host) => {
    host.innerHTML = '';
    AVAILABLE.forEach((l) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.dataset.lang = l.code;
      b.textContent = l.label;
      b.title = l.name;
      b.setAttribute('aria-label', l.name);
      b.addEventListener('click', () => setLanguage(l.code));
      host.appendChild(b);
    });
  });
  paintSwitcher();
}

/**
 * init resolves the language, loads what it needs, and renders.
 *
 * Order of precedence: ?lang= in the URL, then the remembered choice, then
 * English. navigator.language is deliberately NOT consulted — a predictable
 * starting point beats a clever guess that surprises people who share a
 * machine, and the switcher is one click away.
 */
export async function init() {
  try {
    fallback = await loadDict(FALLBACK);
  } catch (err) {
    // The HTML already carries English, so there is nothing to repair: leave
    // the markup exactly as served rather than overwriting it with keys.
    console.warn('i18n: English dictionary unavailable:', err.message);
  }
  dict = fallback;

  let wanted = null;
  try {
    wanted = new URL(window.location.href).searchParams.get('lang');
  } catch (_) {}
  if (!wanted) {
    try { wanted = localStorage.getItem(STORAGE_KEY); } catch (_) {}
  }

  if (wanted && wanted !== FALLBACK && AVAILABLE.some((l) => l.code === wanted)) {
    try {
      dict = await loadDict(wanted);
      current = wanted;
    } catch (_) { /* the language vanished: stay on English */ }
  }

  apply();
  mountSwitcher();
}

init();
