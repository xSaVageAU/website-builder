// The page editor: page settings at the top, then the list of sections.
import { api } from './api.js';
import { h, icon, iconButton, modal, confirmDialog, toast, slugify, uid, clone } from './ui.js';
import { renderFields, defaultsFor, toggle } from './fields.js';

export function mountPageEditor(root, page, app) {
  const schema = app.schema;
  const types = Object.fromEntries(schema.blocks.map((t) => [t.type, t]));
  let blocks = (page.blocks || []).filter((b) => types[b.type]);
  let selected = null;
  const collapsed = new Set();

  const save = () => app.saver.queue(page.id, () => ({
    title: page.title,
    slug: page.slug,
    showInMenu: page.showInMenu,
    published: page.published,
    description: page.description,
    blocks,
  }));
  const ctx = { onChange: save, pages: () => app.pages, toast };

  // ---------- page header ----------

  const titleInput = h('input', { class: 'page-title-input', value: page.title, placeholder: 'Page title', 'aria-label': 'Page title',
    onInput: () => {
      const follows = page.slug === slugify(page.title);
      page.title = titleInput.value;
      if (follows && !page.isHome) { page.slug = slugify(page.title); renderPath(); }
      save();
    } });

  const pathEl = h('div', { class: 'page-path' });
  const parentPath = () => (page.path === '/' ? '/' : page.path.replace(/[^/]+\/$/, ''));
  const renderPath = () => {
    if (page.isHome) {
      pathEl.replaceChildren(icon('home'), h('span', {}, 'Home page · '), h('code', {}, '/'));
      return;
    }
    const slug = h('input', { class: 'slug-input', value: page.slug, 'aria-label': 'Page address', size: Math.max(4, page.slug.length),
      onInput: () => { slug.size = Math.max(4, slug.value.length); },
      onChange: () => { page.slug = slugify(slug.value); slug.value = page.slug; save(); } });
    pathEl.replaceChildren(icon('globe'), h('code', {}, parentPath()), slug, h('code', {}, '/'));
  };
  renderPath();

  const flags = h('div', { class: 'page-flags' },
    toggle(page.published, (v) => { page.published = v; save(); app.refreshTree(); }, 'Published'),
    toggle(page.showInMenu, (v) => { page.showInMenu = v; save(); }, 'Show in menu'),
    h('span', { class: 'spacer' }),
    h('button', { type: 'button', class: 'btn small ghost', onClick: () => showHistory() }, icon('history'), 'History'),
    iconButton('more', 'More page options', (e) => app.pageMenu(page, e.currentTarget)));

  const desc = h('textarea', { class: 'input', rows: 2, maxlength: 300, placeholder: 'One or two sentences about this page.',
    onInput: (e) => { page.description = e.target.value; save(); } });
  desc.value = page.description || '';
  const seo = h('details', { class: 'seo' },
    h('summary', {}, 'Search & sharing'),
    h('div', { class: 'field' },
      h('label', { class: 'field-label' }, 'Description'),
      desc,
      h('div', { class: 'field-help' }, 'Shown in Google results, when the page is shared, and in subpage lists.')));

  // ---------- sections ----------

  const list = h('div', { class: 'blocks' });

  const inserter = (index) => h('div', { class: 'inserter' },
    h('button', { type: 'button', class: 'inserter-btn', title: 'Add a section here', onClick: () => pickBlock(index) }, icon('plus')));

  const card = (b, i) => {
    const t = types[b.type];
    const isCollapsed = collapsed.has(b.id);
    const move = (to) => { blocks.splice(to, 0, blocks.splice(i, 1)[0]); renderBlocks(); save(); };
    const el = h('div', { class: 'block-card' + (selected === b.id ? ' selected' : '') + (isCollapsed ? ' collapsed' : ''), 'data-id': b.id,
      onFocusin: () => select(b.id, true), onClick: () => select(b.id, true) },
      h('div', { class: 'block-head' },
        h('button', { type: 'button', class: 'block-toggle', 'aria-expanded': String(!isCollapsed),
          onClick: (e) => { e.stopPropagation(); isCollapsed ? collapsed.delete(b.id) : collapsed.add(b.id); renderBlocks(); } },
          icon('chevron', 'chev'), icon(t.icon), h('span', { class: 'block-label' }, t.label),
          isCollapsed ? h('span', { class: 'block-summary' }, summary(b)) : null),
        h('span', { class: 'spacer' }),
        i > 0 ? iconButton('up', 'Move up', () => move(i - 1)) : null,
        i < blocks.length - 1 ? iconButton('down', 'Move down', () => move(i + 1)) : null,
        iconButton('copy', 'Duplicate', () => {
          const copy = { ...clone(b), id: uid() };
          blocks.splice(i + 1, 0, copy);
          selected = copy.id;
          renderBlocks();
          save();
        }),
        iconButton('trash', 'Delete section', () => {
          blocks.splice(i, 1);
          renderBlocks();
          save();
          toast(`${t.label} section deleted`, 'info', {
            label: 'Undo',
            onClick: () => { blocks.splice(Math.min(i, blocks.length), 0, b); renderBlocks(); save(); },
          });
        }, 'danger')),
      isCollapsed ? null : h('div', { class: 'block-body' },
        t.fields?.length ? renderFields(t.fields, b.data, ctx) : h('p', { class: 'muted' }, t.description)));
    return el;
  };

  const renderBlocks = () => {
    const children = [];
    blocks.forEach((b, i) => { children.push(inserter(i), card(b, i)); });
    children.push(h('button', { type: 'button', class: 'add-section', onClick: () => pickBlock(blocks.length) },
      icon('plus'), blocks.length ? 'Add a section' : 'Add your first section'));
    list.replaceChildren(...children);
  };

  const select = (id, fromEditor) => {
    if (selected === id) return;
    selected = id;
    list.querySelectorAll('.block-card').forEach((c) => c.classList.toggle('selected', c.dataset.id === id));
    if (fromEditor) app.preview.showBlock(id);
  };

  const pickBlock = (index) => {
    const dlg = modal({
      title: 'Add a section',
      wide: true,
      body: h('div', { class: 'block-picker' }, schema.blocks.map((t) =>
        h('button', { type: 'button', class: 'block-option', onClick: () => {
          const b = { id: uid(), type: t.type, data: defaultsFor(t.fields) };
          blocks.splice(index, 0, b);
          selected = b.id;
          dlg.close();
          renderBlocks();
          save();
          requestAnimationFrame(() => {
            const el = list.querySelector(`[data-id="${b.id}"]`);
            el?.scrollIntoView({ behavior: 'smooth', block: 'center' });
            el?.querySelector('input, textarea, [contenteditable]')?.focus();
          });
        } },
          h('span', { class: 'block-option-icon' }, icon(t.icon)),
          h('span', {}, h('strong', {}, t.label), h('small', {}, t.description))))),
    });
  };

  const showHistory = async () => {
    await app.saver.flush();
    const revs = await api.revisions(page.id);
    const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });
    const dlg = modal({
      title: 'Page history',
      body: h('div', {},
        h('p', { class: 'muted' }, 'Sitesmith keeps a snapshot of this page every few minutes while you edit. Restoring is safe: your current version stays in the history too.'),
        h('ul', { class: 'history' }, revs.map((r, i) => h('li', {},
          h('span', {}, fmt.format(new Date(r.createdAt)), i === 0 ? h('em', { class: 'muted' }, ' — current') : null),
          i === 0 ? null : h('button', { type: 'button', class: 'btn small', onClick: async () => {
            const p = await api.restoreRevision(page.id, r.id);
            dlg.close();
            toast('Earlier version restored');
            app.openPage(p.id, { reload: true });
          } }, 'Restore'))))),
    });
  };

  renderBlocks();
  root.replaceChildren(h('div', { class: 'page-editor' },
    h('div', { class: 'page-header' }, titleInput, pathEl, flags, seo),
    list));

  return {
    page,
    // Called when a section is clicked in the preview.
    selectFromPreview(id) {
      if (collapsed.delete(id)) renderBlocks();
      select(id, false);
      list.querySelector(`[data-id="${id}"]`)?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    },
    // The server may adjust the slug (e.g. "about-2"); show what it chose.
    applySaved(saved) {
      page.path = saved.path;
      if (document.activeElement?.classList.contains('slug-input')) return;
      if (saved.slug !== page.slug) page.slug = saved.slug;
      renderPath();
    },
    setHome(isHome) { page.isHome = isHome; renderPath(); },
  };
}

function summary(b) {
  const d = b.data || {};
  const s = d.heading || d.text || d.label || (d.html || '').replace(/<[^>]+>/g, ' ') || '';
  return s.trim().slice(0, 50);
}
