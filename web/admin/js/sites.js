// "Your sites": switch between sites, start a new one, or remove one.
import { api } from './api.js';
import { h, icon, modal, confirmDialog, toast } from './ui.js';

const PREV = 'sitesmith.prevSite';

function session(k, v) {
  try {
    if (v === undefined) return sessionStorage.getItem(k);
    if (v === null) sessionStorage.removeItem(k); else sessionStorage.setItem(k, v);
  } catch { return null; }
}

// Page ids belong to one site, so forget the last open page and start fresh.
function reloadInto() {
  try { localStorage.removeItem('sitesmith.page'); } catch { /* private mode */ }
  location.reload();
}

export async function switchSite(id, beforeLeave) {
  await beforeLeave?.();
  await api.openSite(id);
  session(PREV, null);
  reloadInto();
}

export async function newSite(beforeLeave) {
  await beforeLeave?.();
  const list = await api.sites();
  const cur = list.find((s) => s.current);
  if (cur) session(PREV, cur.id);
  await api.createSite();
  reloadInto();
}

// From the setup wizard: drop the empty new site and go back to the previous one.
export async function cancelNewSite() {
  const list = await api.sites();
  const cur = list.find((s) => s.current);
  const prev = list.find((s) => s.id === session(PREV)) || list.find((s) => !s.current && s.setupDone);
  if (!prev) return;
  await api.openSite(prev.id);
  if (cur && !cur.setupDone) await api.removeSite(cur.id);
  session(PREV, null);
  reloadInto();
}

// Whether the wizard should offer a way back.
export async function hasOtherSites() {
  const list = await api.sites().catch(() => []);
  return list.some((s) => !s.current && s.setupDone);
}

const when = (d) => {
  const t = new Date(d);
  const days = Math.round((Date.now() - t) / 86400000);
  if (days < 1) return 'today';
  if (days === 1) return 'yesterday';
  if (days < 30) return `${days} days ago`;
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(t);
};

export async function showSites(beforeLeave) {
  const listEl = h('div', { class: 'site-list' });
  const render = async () => {
    const list = await api.sites();
    listEl.replaceChildren(...list.map((s) => h('div', { class: 'site-row' + (s.current ? ' current' : '') },
      h('span', { class: 'site-avatar' }, (s.setupDone ? s.name : '?').trim().charAt(0).toUpperCase()),
      h('div', { class: 'site-info' },
        h('strong', {}, s.setupDone ? s.name : 'New site (not set up yet)'),
        h('span', { class: 'muted' }, `Edited ${when(s.updatedAt)}`)),
      s.current
        ? h('span', { class: 'badge on' }, 'Open now')
        : h('div', { class: 'site-actions' },
            h('button', { type: 'button', class: 'btn small', onClick: () => switchSite(s.id, beforeLeave).catch((e) => toast(e.message, 'error')) }, 'Open'),
            h('button', { type: 'button', class: 'icon-btn danger', title: 'Remove site', 'aria-label': 'Remove site', onClick: async () => {
              const ok = await confirmDialog(
                `“${s.setupDone ? s.name : 'New site'}” will be removed from your sites. Its files are moved to the trash folder inside your Sitesmith data folder, so you can still get them back.`,
                { okLabel: 'Remove', danger: true, title: 'Remove site?' });
              if (!ok) return;
              try { await api.removeSite(s.id); toast('Site removed'); render(); } catch (e) { toast(e.message, 'error'); }
            } }, icon('trash'))))));
  };
  modal({
    title: 'Your sites',
    body: h('div', {},
      h('p', { class: 'muted' }, 'Each site has its own pages, images and design. Switching saves your changes first.'),
      listEl),
    actions: [{ label: 'New site', kind: 'primary', onClick: () => { newSite(beforeLeave).catch((e) => toast(e.message, 'error')); } }],
  });
  await render();
}
