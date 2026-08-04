// SentinelDesk
// A collaborative operating system for people and AI agents.
//
// Copyright 2026 Federico Pereira <fpereira@cnsoluciones.com>
// Co-authored by Nicolas Pereira <npereira@cnsoluciones.com>
//
// Licensed under the Apache License, Version 2.0.
//
// This product's name and logo are trademarks of Federico Pereira and are not
// covered by the license above. See the README for the trademark policy.
//
// SPDX-License-Identifier: Apache-2.0


/* Translations.
 *
 * English is always the default and always the fallback: a missing key shows
 * the English string rather than a raw identifier, so a half-finished
 * translation degrades into a readable mix instead of gibberish.
 *
 * The language list is not hard-coded here. The server enumerates the files in
 * assets/lang and serves the result at /lang/index.json, so dropping another
 * JSON into that directory is all it takes to add a language — no change to
 * this file, and none to the markup.
 *
 * Static text is marked up in the HTML:
 *   data-i18n="key"              replaces the element's text
 *   data-i18n-html="key"         same, but the value may contain markup
 *   data-i18n-title="key"        replaces the title attribute
 *   data-i18n-placeholder="key"  replaces the placeholder
 */

const STORAGE_KEY = 'sentineldesk_lang';
const FALLBACK = 'en';

let dict = {};      // active language
let fallback = {};  // English, always loaded
let current = FALLBACK;
let available = [{ code: FALLBACK, name: 'English' }];

/** t returns a translated string, filling {placeholders} from vars. */
export function t(key, vars) {
  let s = dict[key] ?? fallback[key] ?? key;
  if (vars) {
    for (const [k, v] of Object.entries(vars)) s = s.split('{' + k + '}').join(v);
  }
  return s;
}

export function languages() { return available; }
export function currentLanguage() { return current; }

async function loadDict(code) {
  const res = await fetch('/lang/' + encodeURIComponent(code) + '.json');
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
  root.querySelectorAll('[data-i18n-placeholder]').forEach((el) => {
    el.placeholder = t(el.dataset.i18nPlaceholder);
  });
  document.documentElement.lang = current;
}

/**
 * setLanguage switches language, remembers the choice, and re-renders.
 * Returns false when the language could not be loaded, leaving the previous
 * one in place — a failed switch must not blank the interface.
 */
export async function setLanguage(code) {
  if (code === current) return true;
  try {
    dict = code === FALLBACK ? fallback : await loadDict(code);
  } catch (err) {
    console.warn('i18n:', err.message);
    return false;
  }
  current = code;
  try { localStorage.setItem(STORAGE_KEY, code); } catch (_) { /* private mode */ }
  apply();
  document.dispatchEvent(new CustomEvent('languagechange'));
  return true;
}

/**
 * init loads English plus whatever was chosen last time.
 *
 * English is the default on a first visit — deliberately, rather than sniffing
 * navigator.language: a predictable starting point beats a clever guess that
 * surprises people who share a machine.
 */
export async function init() {
  try {
    fallback = await loadDict(FALLBACK);
  } catch (err) {
    console.warn('i18n: English dictionary unavailable:', err.message);
  }
  dict = fallback;

  try {
    const res = await fetch('/lang/index.json');
    if (res.ok) available = await res.json();
  } catch (_) { /* keep the built-in single entry */ }

  let saved = null;
  try { saved = localStorage.getItem(STORAGE_KEY); } catch (_) {}
  if (saved && saved !== FALLBACK && available.some((l) => l.code === saved)) {
    try {
      dict = await loadDict(saved);
      current = saved;
    } catch (_) { /* the saved language vanished: stay on English */ }
  }
  apply();
}
