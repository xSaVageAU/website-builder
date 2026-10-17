// Versions: whole-site snapshots you can go back to, plus "discard changes".
import { api } from './api.js';
import { h, icon, modal, confirmDialog, toast } from './ui.js';

const timeFmt = new Intl.DateTimeFormat(undefined, { timeStyle: 'short' });
const dayFmt = new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long' });
const fullFmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });

function dayLabel(d) {
  const today = new Date();
  const yesterday = new Date(Date.now() - 86400000);
  if (d.toDateString() === today.toDateString()) return 'Today';
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday';
  return dayFmt.format(d);
}

function describe(v) {
  switch (v.kind) {
    case 'published': return { title: 'Published', icon: 'publish' };
    case 'saved': return { title: v.label || 'Saved version', icon: 'check' };
    case 'restore': return { title: 'Before going back to an earlier version', icon: 'history' };
    default: return { title: 'Automatic snapshot', icon: 'history' };
  }
}

// Words for the change count, e.g. on the Publish button's tooltip.
export function changesText(c) {
  if (!c?.everPublished) return 'Not published yet';
  if (!c.count) return 'Everything is published';
  return c.count === 1 ? '1 unpublished change' : `${c.count} unpublished changes`;
}

// Restore and discard replace everything the editor has loaded, so start fresh.
const reload = () => location.reload();

// Confirms going back. Only when the current state isn't in any version yet
// does it ask whether to keep it. Resolves to { save } or null if cancelled.
export async function askGoBack({ title, message, okLabel, danger = false }) {
  const { current } = await api.versions();
  if (current) {
    const ok = await confirmDialog(`${message} What you have now is already in your versions, so you can come back to it.`,
      { title, okLabel, danger });
    return ok ? { save: false } : null;
  }
  return new Promise((resolve) => {
    let choice = null;
    modal({
      title,
      body: h('div', {},
        h('p', { class: 'confirm-text' }, message),
        h('p', { class: 'confirm-text' }, h('strong', {}, 'You have changes that aren’t saved in any version.'),
          ' Save them first so you can come back to them?')),
      actions: [
        { label: 'Cancel' },
        { label: 'Go back without saving', kind: 'ghost danger-text', onClick: () => { choice = { save: false }; } },
        { label: 'Save and go back', kind: 'primary', onClick: () => { choice = { save: true }; } },
      ],
      onClose: () => resolve(choice),
    });
  });
}

export async function discardAll(app) {
  await app.saver.flush();
  const { versions, changes } = await api.versions();
  const live = versions.find((v) => v.live);
  if (!changes.everPublished || !changes.count) return toast(changesText(changes));
  const choice = await askGoBack({
    title: `Discard ${changesText(changes)}?`,
    message: `Everything goes back to how it was when you last published${live ? ` (${fullFmt.format(new Date(live.createdAt))})` : ''}.`,
    okLabel: 'Discard changes',
    danger: true,
  });
  if (!choice) return;
  try { await api.discardChanges(choice.save); reload(); } catch (e) { toast(e.message, 'error'); }
}

export async function showVersions(app) {
  await app.saver.flush();
  const status = h('p', { class: 'versions-status' });
  const listEl = h('div', { class: 'version-list' });
  const discardBtn = h('button', { type: 'button', class: 'btn', onClick: () => discardAll(app) }, 'Discard unpublished changes');

  const render = async () => {
    const { versions, changes, current } = await api.versions();
    status.textContent = changesText(changes) + '.';
    discardBtn.disabled = !changes.everPublished || !changes.count;
    if (!versions.length) {
      listEl.replaceChildren(h('p', { class: 'muted' }, 'No versions yet. Sitesmith takes one automatically every few minutes while you make changes, and every time you publish.'));
      return;
    }
    let day = '';
    const rows = [];
    // Where you are: either one of the versions below, or unsaved work above them.
    if (!current) {
      rows.push(h('div', { class: 'version-row here' },
        h('span', { class: 'version-time' }, 'Now'),
        icon('text'),
        h('span', { class: 'version-title' }, 'Your current work, not saved in a version yet'),
        h('span', { class: 'badge here' }, 'You’re here')));
    }
    for (const v of versions) {
      const when = new Date(v.createdAt);
      const d = dayLabel(when);
      if (d !== day) { day = d; rows.push(h('h3', { class: 'version-day' }, d)); }
      const info = describe(v);
      const isHere = v.id === current;
      rows.push(h('div', { class: 'version-row' + (v.live ? ' live' : '') + (isHere ? ' here' : '') },
        h('span', { class: 'version-time' }, timeFmt.format(when)),
        icon(info.icon),
        h('span', { class: 'version-title' }, info.title),
        v.live ? h('span', { class: 'badge on', title: 'This is what your published site shows' }, 'Live') : null,
        isHere ? h('span', { class: 'badge here', title: 'The editor shows exactly this version' }, 'You’re here') : null,
        h('span', { class: 'spacer' }),
        isHere ? null : h('button', { type: 'button', class: 'btn small', onClick: () => restore(v, when) }, 'Go back to this')));
    }
    listEl.replaceChildren(...rows);
  };

  const restore = async (v, when) => {
    const choice = await askGoBack({
      title: 'Go back to this version?',
      message: `Your whole site (pages, menu, design and settings) goes back to how it was on ${fullFmt.format(when)}.`,
      okLabel: 'Go back',
    });
    if (!choice) return;
    try { await api.restoreVersion(v.id, choice.save); reload(); } catch (e) { toast(e.message, 'error'); }
  };

  const save = () => {
    let label = '';
    modal({
      title: 'Save a version',
      body: h('div', {},
        h('p', { class: 'muted' }, 'Give it a name so it’s easy to find, like “Before the redesign”.'),
        h('input', { class: 'input', autofocus: true, maxlength: 100, placeholder: 'Name (optional)', onInput: (e) => { label = e.target.value; } })),
      actions: [
        { label: 'Cancel' },
        { label: 'Save version', kind: 'primary', onClick: () => {
          api.saveVersion(label).then(() => { toast('Version saved'); render(); }).catch((e) => toast(e.message, 'error'));
        } },
      ],
    });
  };

  modal({
    title: 'Versions',
    wide: true,
    body: h('div', { class: 'versions' },
      h('p', { class: 'muted' },
        'Your work saves automatically, but nothing goes live until you publish. Sitesmith keeps snapshots of the whole site, so you can always go back.'),
      h('div', { class: 'versions-bar' },
        status,
        h('span', { class: 'spacer' }),
        discardBtn,
        h('button', { type: 'button', class: 'btn primary', onClick: save }, icon('plus'), 'Save version')),
      listEl),
  });
  await render();
}
