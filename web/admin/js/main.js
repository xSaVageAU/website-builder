// Editor shell: wires the sidebar, the editor column and the live preview together.
import { api } from './api.js';
import { h, icon, iconButton, modal, confirmDialog, toast, debounce, fill } from './ui.js';
import { renderTree } from './tree.js';
import { mountPageEditor } from './editor.js';
import { mountDesign, mountSettings } from './design.js';
import { mountMediaLibrary, loadMedia } from './media.js';
import { runWizard } from './wizard.js';

const store = {
  get(k) { try { return localStorage.getItem(k); } catch { return null; } },
  set(k, v) { try { localStorage.setItem(k, v); } catch { /* private mode */ } },
};

const app = {
  schema: null,
  site: null,
  pages: [],
  currentId: null,
  view: 'page',
  editor: null,
};

// ---------- saving ----------

const status = h('div', { class: 'save-status', 'aria-live': 'polite' });
function setStatus(kind, msg) {
  status.dataset.kind = kind;
  status.replaceChildren(kind === 'saved' ? icon('check') : kind === 'error' ? icon('warn') : h('span', { class: 'dot' }),
    msg || { saved: 'All changes saved', saving: 'Saving…', dirty: 'Unsaved changes', error: "Couldn't save" }[kind]);
}

// Batches page saves: edits are sent shortly after typing stops, one request
// at a time, and retried if the server can't be reached.
app.saver = (() => {
  const pending = new Map(); // page id -> () => payload
  let timer = null;
  let running = null;

  async function run() {
    const jobs = [...pending];
    pending.clear();
    setStatus('saving');
    for (const [id, payload] of jobs) {
      try {
        const saved = await api.updatePage(id, payload());
        onPageSaved(saved);
      } catch (e) {
        if (e.status === 404) continue; // page was deleted meanwhile
        if (!pending.has(id)) pending.set(id, payload);
        setStatus('error', "Couldn't save. Retrying…");
        timer = setTimeout(flush, 4000);
        return;
      }
    }
    setStatus(pending.size ? 'dirty' : 'saved');
  }

  async function flush() {
    clearTimeout(timer);
    while (running) await running;
    if (!pending.size) return;
    running = run();
    await running;
    running = null;
  }

  return {
    queue(id, payload) {
      pending.set(id, payload);
      setStatus('dirty');
      clearTimeout(timer);
      timer = setTimeout(flush, 600);
    },
    flush,
    busy: () => pending.size > 0 || running != null,
  };
})();

const refreshAfterSave = debounce(async () => {
  await app.refreshTree();
  app.preview.refresh();
}, 150);

function onPageSaved(saved) {
  if (app.editor?.page.id === saved.id) {
    app.editor.applySaved(saved);
    app.preview.show(saved.path); // the address changes when the slug does
  }
  refreshAfterSave();
}

const saveSiteNow = async () => {
  try {
    setStatus('saving');
    // Keep app.site as the one object the panels edit: replacing it with the
    // response would leave open panels editing a stale copy, and merging the
    // response could overwrite keystrokes typed while the request was in flight.
    await api.saveSite(app.site);
    setStatus('saved');
    app.preview.refresh();
  } catch (e) {
    setStatus('error', e.message);
  }
};
const saveSiteSoon = debounce(saveSiteNow, 400);
app.saveSite = (immediate = false) => {
  setStatus('dirty');
  if (immediate) { saveSiteSoon.cancel(); return saveSiteNow(); }
  saveSiteSoon();
};

// ---------- preview ----------

app.preview = (() => {
  const frame = h('iframe', { class: 'preview-frame', title: 'Preview of your page' });
  const wrap = h('div', { class: 'frame-wrap' }, frame);
  let path = null;
  let selected = null;
  let seq = 0;
  let device = 'desktop';

  // The preview column is narrower than a laptop screen, so "desktop" renders
  // the page at a real desktop width and scales it down to fit.
  const DESKTOP = 1280, PAD = 14;
  const fit = () => {
    const w = wrap.clientWidth - PAD * 2;
    const ht = wrap.clientHeight - PAD * 2;
    if (device === 'mobile') {
      Object.assign(frame.style, { width: '390px', height: ht + 'px', transform: '', left: Math.max(PAD, (wrap.clientWidth - 390) / 2) + 'px' });
      return;
    }
    const s = Math.min(1, w / DESKTOP);
    Object.assign(frame.style, { width: (s < 1 ? DESKTOP : w) + 'px', height: ht / s + 'px', transform: s < 1 ? `scale(${s})` : '', left: PAD + 'px' });
  };
  new ResizeObserver(fit).observe(wrap);

  frame.addEventListener('load', () => {
    let p;
    try { p = frame.contentWindow.location.pathname; } catch { return; }
    if (!p.startsWith('/preview/')) return;
    p = decodeURIComponent(p.slice('/preview'.length));
    if (p !== path) {
      // A link inside the preview was clicked: open that page in the editor.
      path = p;
      const page = app.pages.find((x) => x.path === p);
      if (page && page.id !== app.currentId) app.openPage(page.id, { fromPreview: true });
    }
    if (selected) post({ type: 'show-block', id: selected, scroll: false });
  });

  window.addEventListener('message', (e) => {
    if (e.source !== frame.contentWindow || e.data?.type !== 'select-block') return;
    selected = e.data.id;
    app.editor?.selectFromPreview(e.data.id);
  });

  const post = (msg) => frame.contentWindow?.postMessage(msg, location.origin);

  return {
    el: wrap,
    show(p) {
      if (p === path) return;
      path = p;
      selected = null;
      frame.src = '/preview' + encodeURI(p);
    },
    showBlock(id) {
      selected = id;
      post({ type: 'show-block', id });
    },
    // Re-renders in place: no flash and the scroll position is kept.
    async refresh() {
      if (!path) return;
      const mine = ++seq;
      let html;
      try {
        const res = await fetch('/preview' + encodeURI(path), { cache: 'no-store' });
        if (!res.ok) return;
        html = await res.text();
      } catch { return; }
      if (mine !== seq) return;
      const doc = frame.contentDocument;
      if (!doc?.body) return;
      const next = new DOMParser().parseFromString(html, 'text/html');
      const oldCSS = doc.querySelector('link[rel=stylesheet]');
      const newCSS = next.querySelector('link[rel=stylesheet]');
      if (oldCSS && newCSS && oldCSS.getAttribute('href') !== newCSS.getAttribute('href')) {
        const l = doc.createElement('link');
        l.rel = 'stylesheet';
        l.href = newCSS.getAttribute('href');
        l.onload = () => oldCSS.remove();
        oldCSS.after(l);
      }
      doc.title = next.title;
      next.body.querySelectorAll('script').forEach((s) => s.remove());
      doc.body.className = next.body.className;
      doc.body.innerHTML = next.body.innerHTML;
      if (selected) post({ type: 'show-block', id: selected, scroll: false });
    },
    setDevice(d) { device = d; fit(); },
  };
})();

// ---------- layout ----------

const treeEl = h('div', { class: 'tree' });
const main = h('main', { class: 'editor-col' });
const siteTitle = h('div', { class: 'site-title' });
const sideNav = h('nav', { class: 'side-nav' });

app.renderTopbar = () => { siteTitle.textContent = app.site.name; };

function renderSideNav() {
  const item = (view, label, ic) => h('button', { type: 'button', class: 'side-link' + (app.view === view ? ' on' : ''),
    onClick: () => setView(view) }, icon(ic), label);
  sideNav.replaceChildren(item('design', 'Design', 'palette'), item('media', 'Media', 'image'), item('settings', 'Settings', 'settings'));
}

async function setView(view) {
  await app.saver.flush();
  app.view = view;
  if (view !== 'page') app.editor = null;
  renderSideNav();
  app.renderTree();
  if (view === 'design') mountDesign(main, app);
  if (view === 'settings') mountSettings(main, app);
  if (view === 'media') mountMediaLibrary(main);
  if (view === 'page' && app.currentId) app.openPage(app.currentId, { reload: true });
}

const deviceSeg = h('div', { class: 'seg device' });
function renderDevice(d) {
  deviceSeg.replaceChildren(
    h('button', { type: 'button', class: d === 'desktop' ? 'on' : '', title: 'Desktop preview', onClick: () => { renderDevice('desktop'); app.preview.setDevice('desktop'); } }, icon('monitor')),
    h('button', { type: 'button', class: d === 'mobile' ? 'on' : '', title: 'Phone preview', onClick: () => { renderDevice('mobile'); app.preview.setDevice('mobile'); } }, icon('phone')));
}

function layout() {
  renderDevice('desktop');
  document.getElementById('app').replaceChildren(
    h('header', { class: 'topbar' },
      h('div', { class: 'brand' }, h('span', { class: 'logo-dot' }), 'Sitesmith'),
      siteTitle,
      h('span', { class: 'spacer' }),
      status,
      deviceSeg,
      h('button', { type: 'button', class: 'btn primary', onClick: publish }, icon('publish'), 'Publish')),
    h('aside', { class: 'sidebar' },
      h('div', { class: 'side-head' },
        h('span', {}, 'Pages'),
        iconButton('plus', 'Add a page', () => addPage(0))),
      treeEl,
      sideNav),
    main,
    h('section', { class: 'preview-col' }, app.preview.el));
}

// ---------- pages ----------

app.refreshTree = async () => {
  const res = await api.pages();
  app.pages = res.pages;
  app.renderTree();
};

app.renderTree = () => {
  renderTree(treeEl, app.pages, {
    currentId: app.view === 'page' ? app.currentId : null,
    onSelect: (id) => { app.view = 'page'; renderSideNav(); app.openPage(id); },
    onAdd: addPage,
    onMenu: app.pageMenu,
    onMove: async (id, parentId, index) => {
      await app.saver.flush();
      try {
        const res = await api.movePage(id, parentId, index);
        app.pages = res.pages;
        app.renderTree();
        const p = app.pages.find((x) => x.id === app.currentId);
        if (p && app.editor) {
          app.editor.applySaved(p);
          app.preview.show(p.path);
        }
        app.preview.refresh();
      } catch (e) { toast(e.message, 'error'); }
    },
  });
};

app.openPage = async (id, { reload = false, fromPreview = false } = {}) => {
  if (!reload && app.view === 'page' && app.editor?.page.id === id) return;
  await app.saver.flush();
  let page;
  try { page = await api.page(id); } catch (e) { return toast(e.message, 'error'); }
  const item = app.pages.find((p) => p.id === id);
  page.isHome = !!item?.isHome;
  app.view = 'page';
  app.currentId = id;
  store.set('sitesmith.page', String(id));
  renderSideNav();
  app.editor = mountPageEditor(main, page, app);
  app.renderTree();
  if (!fromPreview) app.preview.show(page.path);
  main.scrollTop = 0;
};

async function addPage(parentId) {
  await app.saver.flush();
  try {
    const p = await api.createPage(parentId, 'New page');
    await app.refreshTree();
    await app.openPage(p.id);
    const t = main.querySelector('.page-title-input');
    t?.focus();
    t?.select();
  } catch (e) { toast(e.message, 'error'); }
}

app.pageMenu = (page, anchor) => {
  const item = app.pages.find((p) => p.id === page.id) || page;
  const close = () => { menu.remove(); document.removeEventListener('mousedown', outside, true); };
  const outside = (e) => { if (!menu.contains(e.target)) close(); };
  const entry = (ic, label, fn, cls = '') => h('button', { type: 'button', class: 'menu-item ' + cls, onClick: () => { close(); fn(); } }, icon(ic), label);
  const menu = h('div', { class: 'popover-menu', role: 'menu' },
    entry('plus', 'Add a subpage', () => addPage(item.id)),
    entry('copy', 'Duplicate', async () => {
      await app.saver.flush();
      const p = await api.duplicatePage(item.id);
      await app.refreshTree();
      app.openPage(p.id);
      toast('Copy created as a draft');
    }),
    !item.parentId && !item.isHome ? entry('home', 'Make this the home page', async () => {
      app.site.homePageId = item.id;
      await app.saveSite(true);
      await app.refreshTree();
      if (app.editor) app.editor.setHome(app.editor.page.id === item.id);
      toast(`"${item.title}" is now the home page`);
    }) : null,
    item.isHome ? null : entry('trash', 'Delete page', () => deletePage(item), 'danger'));
  document.body.append(menu);
  const r = anchor.getBoundingClientRect();
  menu.style.top = Math.min(r.bottom + 4, innerHeight - menu.offsetHeight - 8) + 'px';
  menu.style.left = Math.min(r.left, innerWidth - menu.offsetWidth - 8) + 'px';
  setTimeout(() => document.addEventListener('mousedown', outside, true), 0);
};

async function deletePage(page) {
  const count = (id) => app.pages.filter((p) => p.parentId === id).reduce((n, p) => n + 1 + count(p.id), 0);
  const n = count(page.id);
  const ok = await confirmDialog(
    n ? `"${page.title}" and the ${n} page${n > 1 ? 's' : ''} inside it will be deleted. This can't be undone.`
      : `"${page.title}" will be deleted. This can't be undone.`,
    { okLabel: 'Delete', danger: true, title: 'Delete page?' });
  if (!ok) return;
  await app.saver.flush();
  try {
    await api.deletePage(page.id);
  } catch (e) { return toast(e.message, 'error'); }
  await app.refreshTree();
  if (!app.pages.some((p) => p.id === app.currentId)) {
    const home = app.pages.find((p) => p.isHome) || app.pages[0];
    if (home) app.openPage(home.id); else main.replaceChildren();
  }
  toast('Page deleted');
}

// ---------- publishing ----------

async function publish() {
  await app.saver.flush();
  const body = h('div', { class: 'publish' }, h('div', { class: 'publish-busy' }, h('span', { class: 'spinner' }), 'Building your site…'));
  modal({ title: 'Publish', body, wide: true });
  let rep;
  try {
    rep = await api.publish();
  } catch (e) {
    body.replaceChildren(h('p', { class: 'error-text' }, e.message));
    return;
  }
  fill(body,
    h('div', { class: 'publish-done' },
      h('span', { class: 'big-check' }, icon('check')),
      h('div', {},
        h('h3', {}, 'Your site is ready'),
        h('p', { class: 'muted' }, `${rep.pages} page${rep.pages === 1 ? '' : 's'}${rep.media ? `, ${rep.media} image${rep.media === 1 ? '' : 's'}` : ''} · built in ${rep.duration}`))),
    rep.warnings.length ? h('div', { class: 'warnings' },
      h('strong', {}, icon('warn'), 'Worth checking'),
      h('ul', {}, rep.warnings.map((w) => h('li', {}, w)))) : null,
    h('div', { class: 'publish-actions' },
      h('a', { class: 'btn primary', href: '/site/', target: '_blank', rel: 'noopener' }, icon('external'), 'View published site'),
      h('a', { class: 'btn', href: '/api/export', download: '' }, icon('download'), 'Download as ZIP'),
      h('button', { type: 'button', class: 'btn', onClick: () => api.openFolder().catch((e) => toast(e.message, 'error')) }, icon('folder'), 'Open folder')),
    h('details', { class: 'hosting' },
      h('summary', {}, 'How do I put it online for free?'),
      h('p', {}, 'Your site is a folder of ordinary files, so it works with any web host. Some free options:'),
      h('ul', {},
        h('li', {}, h('strong', {}, 'Netlify Drop'), ' — drag the published folder onto ', h('a', { href: 'https://app.netlify.com/drop', target: '_blank', rel: 'noopener' }, 'app.netlify.com/drop'), '.'),
        h('li', {}, h('strong', {}, 'Cloudflare Pages'), ' — create a project and upload the folder or the ZIP.'),
        h('li', {}, h('strong', {}, 'GitHub Pages'), ' — upload the files to a repository and turn on Pages.'),
        h('li', {}, h('strong', {}, 'Any web host'), ' — upload the contents of the ZIP with their file manager or FTP.')),
      h('p', { class: 'muted' }, `The folder is at ${rep.outputDir}. Publish again whenever you make changes, then upload the new files.`)));
}

// ---------- start ----------

async function start() {
  app.schema = await api.schema();
  app.site = await api.site();
  if (!app.site.setupDone) {
    await runWizard(app.schema);
    app.site = await api.site();
  }
  await loadMedia();
  layout();
  app.renderTopbar();
  renderSideNav();
  setStatus('saved');
  await app.refreshTree();
  const last = Number(store.get('sitesmith.page'));
  const first = app.pages.find((p) => p.id === last) || app.pages.find((p) => p.isHome) || app.pages[0];
  if (first) app.openPage(first.id);

  document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
      e.preventDefault();
      app.saver.flush();
    }
  });
  window.addEventListener('beforeunload', (e) => { if (app.saver.busy()) e.preventDefault(); });
}

start().catch((e) => {
  document.getElementById('app').replaceChildren(h('div', { class: 'fatal' }, h('h1', {}, 'Something went wrong'), h('p', {}, e.message)));
});
