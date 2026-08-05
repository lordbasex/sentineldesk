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


/* The documentation viewer.
 *
 * One page, three translations, and a table of contents built from whatever the
 * translation actually contains. The alternative — a hand-written nav per
 * language — goes stale the first time somebody adds a section to one file and
 * forgets the other two.
 *
 * Deliberately a plain script and not a module: it is loaded straight from the
 * binary with no build step, and there is nothing here worth a bundler.
 */

var LANGS = [
  { code: 'en', label: 'EN', name: 'English' },
  { code: 'es', label: 'ES', name: 'Español' },
  { code: 'pt', label: 'PT', name: 'Português' }
];

/* Shared with the desktop client, so choosing Spanish in the toolbar means the
   docs open in Spanish too. */
var STORAGE_KEY = 'sentineldesk_lang';

var TAGLINE = {
  en: 'A collaborative operating system for people and AI agents.',
  es: 'Un sistema operativo colaborativo para personas y agentes de IA.',
  pt: 'Um sistema operacional colaborativo para pessoas e agentes de IA.'
};

var BACK = {
  en: '← Back to the project site',
  es: '← Volver al sitio del proyecto',
  pt: '← Voltar ao site do projeto'
};

/* The three labels in the bar. A table in the same shape as the two above
 * rather than a second translation mechanism — and the wording is copied from
 * the site's own dictionaries, because the bar leads to those pages and a link
 * that renames its destination is a link people distrust. */
var NAV = {
  en: { arch: 'Architecture', install: 'Installation', start: 'Get started' },
  es: { arch: 'Arquitectura',  install: 'Instalación',  start: 'Empezar' },
  pt: { arch: 'Arquitetura',   install: 'Instalação',   start: 'Começar' }
};

/* The two guides. Sections declare which one they belong to; this only names
   them, so adding a section is one attribute and nothing else. */
var TRACKS = {
  en: { install: 'Installation guide', use: 'Usage guide' },
  es: { install: 'Guía de instalación', use: 'Guía de uso' },
  pt: { install: 'Guia de instalação', use: 'Guia de uso' }
};

var COPY = { en: 'copy', es: 'copiar', pt: 'copiar' };
var COPIED = { en: 'copied', es: 'copiado', pt: 'copiado' };

var current = 'en';

/* pickLanguage: the URL wins, then whatever was chosen in the toolbar, then the
 * browser's own preference. The URL comes first because that is what a link
 * into the docs is FOR — /docs/?lang=es#capture-stream-out has to open in
 * Spanish even for somebody whose toolbar is in English. */
function pickLanguage() {
  var codes = LANGS.map(function (l) { return l.code; });
  var fromURL = new URLSearchParams(location.search).get('lang');
  if (fromURL && codes.indexOf(fromURL) >= 0) return fromURL;

  var saved = null;
  try { saved = localStorage.getItem(STORAGE_KEY); } catch (_) {}
  if (saved && codes.indexOf(saved) >= 0) return saved;

  var nav = (navigator.language || 'en').slice(0, 2).toLowerCase();
  return codes.indexOf(nav) >= 0 ? nav : 'en';
}

function buildLangButtons() {
  var box = document.getElementById('langs');
  box.replaceChildren();
  LANGS.forEach(function (l) {
    var b = document.createElement('button');
    b.type = 'button';
    b.textContent = l.label;
    b.title = l.name;
    b.setAttribute('aria-pressed', String(l.code === current));
    b.addEventListener('click', function () { setLanguage(l.code); });
    box.appendChild(b);
  });
}

/* buildTOC reads the document that was just loaded.
 *
 * The document's own shape becomes the navigation's: a section heading is a
 * group, its sub-headings are the entries under it. Sections carry the ids that
 * outside links point at, so there is no parallel list that can disagree with
 * them — and no third file to update when a section is added.
 *
 * The group label is itself a link. Every section has prose before its first
 * sub-heading, and a label that cannot be clicked would leave that prose with
 * no way to reach it.
 */
function buildTOC() {
  var toc = document.getElementById('toc');
  toc.replaceChildren();

  var names = TRACKS[current] || TRACKS.en;
  var track = null;

  document.querySelectorAll('#content section').forEach(function (sec) {
    if (!sec.id) return;
    var heading = sec.querySelector(':scope > h2') || sec.querySelector(':scope > h1');
    if (!heading) return;

    // A divider where one guide ends and the next begins. The opening sections
    // belong to neither: they are what you read before choosing.
    if (sec.dataset.track && sec.dataset.track !== track) {
      track = sec.dataset.track;
      var head = document.createElement('div');
      head.className = 'track';
      head.textContent = names[track] || track;
      toc.appendChild(head);
    }

    var subs = sec.querySelectorAll(':scope > h3[id]');
    var a = tocLink(sec.id, heading.textContent);
    if (subs.length) a.classList.add('group');
    toc.appendChild(a);

    subs.forEach(function (h3) { toc.appendChild(tocLink(h3.id, h3.textContent)); });
  });
}

function tocLink(id, text) {
  var a = document.createElement('a');
  a.href = '#' + id;
  a.textContent = text;
  a.dataset.target = id;
  return a;
}

/* Highlights the section being read. IntersectionObserver rather than a scroll
 * handler: the browser does the work, and it stays correct when the page is
 * jumped to by anchor instead of scrolled. */
var observer = null;
function watchSections() {
  if (observer) observer.disconnect();
  var links = {};
  document.querySelectorAll('#toc a').forEach(function (a) { links[a.dataset.target] = a; });

  observer = new IntersectionObserver(function (entries) {
    entries.forEach(function (e) {
      var a = links[e.target.id];
      if (!a) return;
      if (e.isIntersecting) {
        Object.keys(links).forEach(function (k) { links[k].classList.remove('current'); });
        a.classList.add('current');
      }
    });
  }, { rootMargin: '0px 0px -72% 0px', threshold: 0 });

  document.querySelectorAll('#content section, #content h3[id]').forEach(function (el) {
    observer.observe(el);
  });
}

/* A copy button on every code block. Commands are what people came for, and
 * selecting three wrapped lines with a mouse is a small daily annoyance. */
function addCopyButtons() {
  document.querySelectorAll('#content pre').forEach(function (pre) {
    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'copy';
    btn.textContent = COPY[current] || 'copy';
    btn.addEventListener('click', function () {
      var code = pre.querySelector('code');
      var text = (code ? code.innerText : pre.innerText)
        // Comment lines are there to explain, not to be pasted into a shell.
        .split('\n').filter(function (l) { return l.trim() && l.trim()[0] !== '#'; })
        .join('\n');
      navigator.clipboard.writeText(text).then(function () {
        btn.textContent = COPIED[current] || 'copied';
        setTimeout(function () { btn.textContent = COPY[current] || 'copy'; }, 1400);
      }).catch(function () {});
    });
    pre.appendChild(btn);
  });
}

/* --- tabs ------------------------------------------------------------------
 *
 * A tab group is written in the content as a div of panes, each labelled. The
 * bar is built here rather than in the markup so a translation only has to say
 * WHAT the choices are, not restate the machinery three times.
 *
 * The choice is shared across every group on the page and remembered: somebody
 * on Windows should say so once, not on each code sample and not again
 * tomorrow. A first visit guesses from the browser, which is right often enough
 * that most readers never touch the tabs at all.
 */
var TAB_KEY = 'sentineldesk_docs_tab_';

function guessPlatform() {
  var s = (navigator.userAgentData && navigator.userAgentData.platform) ||
          navigator.platform || navigator.userAgent || '';
  if (/mac|iphone|ipad/i.test(s)) return 'macOS';
  if (/win/i.test(s)) return 'Windows';
  return 'Linux';
}

function preferredTab(group, labels) {
  var saved = null;
  try { saved = localStorage.getItem(TAB_KEY + group); } catch (_) {}
  if (saved && labels.indexOf(saved) >= 0) return saved;
  if (group === 'os') {
    var guess = guessPlatform();
    if (labels.indexOf(guess) >= 0) return guess;
  }
  return labels[0];
}

function selectTab(group, label) {
  try { localStorage.setItem(TAB_KEY + group, label); } catch (_) {}
  document.querySelectorAll('.tabs[data-group="' + group + '"]').forEach(function (box) {
    box.querySelectorAll(':scope > .tab').forEach(function (pane) {
      pane.hidden = pane.dataset.label !== label;
    });
    box.querySelectorAll('.tabbar button').forEach(function (b) {
      b.setAttribute('aria-selected', String(b.dataset.label === label));
      b.tabIndex = b.dataset.label === label ? 0 : -1;
    });
  });
}

function buildTabs() {
  document.querySelectorAll('#content .tabs').forEach(function (box) {
    var group = box.dataset.group || 'default';
    var panes = box.querySelectorAll(':scope > .tab');
    if (!panes.length) return;

    var labels = [];
    panes.forEach(function (pane) { labels.push(pane.dataset.label); });

    var bar = document.createElement('div');
    bar.className = 'tabbar';
    bar.setAttribute('role', 'tablist');

    labels.forEach(function (label) {
      var b = document.createElement('button');
      b.type = 'button';
      b.setAttribute('role', 'tab');
      b.dataset.label = label;
      b.textContent = label;
      b.addEventListener('click', function () { selectTab(group, label); });
      // Arrow keys move between tabs, which is what a tablist is expected to do.
      b.addEventListener('keydown', function (e) {
        var d = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
        if (!d) return;
        e.preventDefault();
        var next = labels[(labels.indexOf(label) + d + labels.length) % labels.length];
        selectTab(group, next);
        bar.querySelector('[data-label="' + next + '"]').focus();
      });
      bar.appendChild(b);
    });

    box.insertBefore(bar, box.firstChild);
    selectTab(group, preferredTab(group, labels));
  });
}

function scrollToHash() {
  if (!location.hash) { window.scrollTo(0, 0); return; }
  var el = document.getElementById(location.hash.slice(1));
  if (el) el.scrollIntoView();
}

async function load(lang) {
  var box = document.getElementById('content');
  try {
    var res = await fetch('content/' + lang + '.html', { cache: 'no-cache' });
    if (!res.ok) throw new Error(res.status + '');
    box.innerHTML = await res.text();
  } catch (err) {
    // Falling back to English beats an empty page: a reader who wanted Spanish
    // is better served by English text than by nothing at all.
    if (lang !== 'en') { await load('en'); return; }
    box.innerHTML = '<p class="loading">Documentation unavailable (' + err.message + ').</p>';
    return;
  }

  current = lang;
  document.documentElement.lang = lang;
  document.getElementById('tagline').textContent = TAGLINE[lang] || TAGLINE.en;
  document.getElementById('back-link').textContent = BACK[lang] || BACK.en;

  var nav = NAV[lang] || NAV.en;
  document.getElementById('nav-arch').textContent = nav.arch;
  document.getElementById('nav-install').textContent = nav.install;
  document.getElementById('nav-start').textContent = nav.start;

  buildLangButtons();
  buildTOC();
  buildTabs();
  addCopyButtons();
  watchSections();
  scrollToHash();
}

async function setLanguage(lang) {
  if (lang === current) return;
  try { localStorage.setItem(STORAGE_KEY, lang); } catch (_) {}
  // Keep the anchor: switching language should leave you on the same section,
  // not back at the top of a document you were halfway through.
  var url = new URL(location.href);
  url.searchParams.set('lang', lang);
  history.replaceState(null, '', url.toString() + location.hash);
  await load(lang);
}

/* --- the contents panel ---------------------------------------------------
 *
 * Collapsible, and it remembers. Somebody reading a long section on a laptop
 * wants the width; somebody navigating wants the list. Neither should have to
 * say so twice.
 *
 * On a narrow screen it starts closed regardless — there is no room for it and
 * the page it covers is the one being read.
 */
var SIDE_KEY = 'sentineldesk_docs_side';
var NARROW = 900;

var openBtn = document.getElementById('side-open');
var closeBtn = document.getElementById('side-close');

function setSide(open, remember) {
  document.body.classList.toggle('side-closed', !open);
  openBtn.setAttribute('aria-expanded', String(open));
  if (remember) {
    try { localStorage.setItem(SIDE_KEY, open ? 'open' : 'closed'); } catch (_) {}
  }
}

openBtn.addEventListener('click', function () { setSide(true, true); });
closeBtn.addEventListener('click', function () { setSide(false, true); });

// On a narrow screen the panel covers the text, so following a link means you
// are done with it.
document.getElementById('toc').addEventListener('click', function (e) {
  if (e.target.tagName === 'A' && window.innerWidth <= NARROW) setSide(false, false);
});

// Esc closes it, which is the reflex for anything overlaying what you were
// reading.
document.addEventListener('keydown', function (e) {
  if (e.key === 'Escape' && !document.body.classList.contains('side-closed')) {
    setSide(false, window.innerWidth > NARROW);
  }
});

(function initSide() {
  if (window.innerWidth <= NARROW) { setSide(false, false); return; }
  var saved = null;
  try { saved = localStorage.getItem(SIDE_KEY); } catch (_) {}
  setSide(saved !== 'closed', false);
})();

window.addEventListener('hashchange', scrollToHash);

load(pickLanguage());
