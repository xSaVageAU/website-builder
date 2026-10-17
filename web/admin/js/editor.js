// The page editor: page settings at the top, then the list of sections.
import { api } from './api.js';
import { h, icon, iconButton, modal, confirmDialog, toast, slugify, uid, clone } from './ui.js';
import { renderFields, defaultsFor, toggle, codeArea } from './fields.js';
import { askGoBack } from './versions.js';

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
    hideTitle: !!page.hideTitle,
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
    // A page starting with a hero never shows this title: the hero headline is its title.
    h('span', { title: 'The page name shown above the first section. Pages that start with a Hero use its headline instead.' },
      toggle(!page.hideTitle, (v) => { page.hideTitle = !v; save(); }, 'Show title')),
    h('span', { class: 'spacer' }),
    iconButton('more', 'More page options', (e) => app.pageMenu(page, e.currentTarget)));

  // "Unpublished changes · Undo" for this page, kept current by setChange.
  const changeEl = h('div', { class: 'page-change', hidden: true });
  const undoPage = async () => {
    await app.saver.flush();
    const choice = await askGoBack({
      title: 'Undo changes to this page?',
      message: 'This page goes back to how it was when you last published. Everything else stays as it is.',
      okLabel: 'Undo changes',
      danger: true,
    });
    if (!choice) return;
    try {
      await api.discardPage(page.id, choice.save);
      toast('Page changes undone');
      await app.refreshTree();
      app.openPage(page.id, { reload: true });
    } catch (e) { toast(e.message, 'error'); }
  };

  const desc = h('textarea', { class: 'input', rows: 2, maxlength: 300, placeholder: 'One or two sentences about this page.',
    onInput: (e) => { page.description = e.target.value; save(); } });
  desc.value = page.description || '';
  // Always visible: it feeds subpage cards, search results and link previews,
  // and people missed it when it sat in a collapsed section.
  const seo = h('div', { class: 'field page-summary' },
    h('label', { class: 'field-label' }, 'Summary'),
    desc,
    h('div', { class: 'field-help' }, 'Shown on subpage cards, in Google results and when the page is shared.'));

  // ---------- sections ----------

  const list = h('div', { class: 'blocks' });

  const inserter = (index) => h('div', { class: 'inserter' },
    h('button', { type: 'button', class: 'inserter-btn', title: 'Add a section here', onClick: () => pickBlock(index) }, icon('plus')));

  const card = (b, i) => {
    // A reusable section's card edits the library copy, so every page using it changes.
    const isRef = b.type === 'shared';
    const shared = isRef ? (app.shared || []).find((s) => s.id === b.data?.ref) : null;
    const inner = shared ? shared.block : b;
    const t = types[inner.type] || types[b.type];
    const onInner = shared ? () => app.saveShared(shared) : save;
    const isCollapsed = collapsed.has(b.id);
    const move = (to) => { blocks.splice(to, 0, blocks.splice(i, 1)[0]); renderBlocks(); save(); };

    let body;
    if (isRef && !shared) {
      body = h('p', { class: 'muted' }, 'This reusable section was deleted from your library. Remove it from this page.');
    } else {
      body = [
        shared ? h('div', { class: 'shared-banner' },
          h('span', {}, shared.uses > 1 ? `Used on ${shared.uses} pages. Changes here update all of them.` : 'Changes here update every page that uses it.'),
          h('span', { class: 'spacer' }),
          h('button', { type: 'button', class: 'link-btn', onClick: () => renameShared(shared) }, 'Rename'),
          h('button', { type: 'button', class: 'link-btn', title: 'Turn this into a normal section that only belongs to this page',
            onClick: () => { blocks[i] = { ...clone(shared.block), id: uid() }; renderBlocks(); save(); app.refreshShared(); toast('Detached: this copy now only belongs to this page'); } }, 'Detach')) : null,
        t.fields?.length ? renderFields(t.fields, inner.data, { ...ctx, onChange: onInner }) : h('p', { class: 'muted' }, t.description),
        inner.type === 'subpages' ? h('p', { class: 'field-help' },
          "Each card shows a subpage's title and the Summary at the top of that page.") : null,
        styleDrawer(inner, t, onInner),
      ];
    }

    const el = h('div', { class: 'block-card' + (selected === b.id ? ' selected' : '') + (isCollapsed ? ' collapsed' : '') + (isRef ? ' reusable' : ''), 'data-id': b.id,
      onFocusin: () => select(b.id, true), onClick: () => select(b.id, true) },
      h('div', { class: 'block-head' },
        h('button', { type: 'button', class: 'block-toggle', 'aria-expanded': String(!isCollapsed),
          onClick: (e) => { e.stopPropagation(); isCollapsed ? collapsed.delete(b.id) : collapsed.add(b.id); renderBlocks(); } },
          icon('chevron', 'chev'), icon(isRef ? 'link' : t.icon),
          h('span', { class: 'block-label' }, shared ? shared.name : t.label),
          isRef ? h('span', { class: 'badge reuse' }, 'Reusable') : null,
          isCollapsed && !isRef ? h('span', { class: 'block-summary' }, summary(b)) : null),
        h('span', { class: 'spacer' }),
        i > 0 ? iconButton('up', 'Move up', () => move(i - 1)) : null,
        i < blocks.length - 1 ? iconButton('down', 'Move down', () => move(i + 1)) : null,
        isRef ? null : iconButton('link', 'Make reusable', () => makeReusable(b, i)),
        iconButton('copy', 'Duplicate', () => {
          const copy = { ...clone(b), id: uid() };
          blocks.splice(i + 1, 0, copy);
          selected = copy.id;
          renderBlocks();
          save();
        }),
        iconButton('trash', isRef ? 'Remove from this page' : 'Delete section', () => {
          blocks.splice(i, 1);
          renderBlocks();
          save();
          toast(isRef ? 'Removed from this page (still in your library)' : `${t.label} section deleted`, 'info', {
            label: 'Undo',
            onClick: () => { blocks.splice(Math.min(i, blocks.length), 0, b); renderBlocks(); save(); },
          });
        }, 'danger')),
      isCollapsed ? null : h('div', { class: 'block-body' }, body));
    return el;
  };

  // Ask for a name, then move the section into the library and leave a
  // reference to it here.
  const askName = (title, intro, value, okLabel) => new Promise((resolve) => {
    let name = value;
    let ok = false;
    modal({
      title,
      body: h('div', {}, intro ? h('p', { class: 'muted' }, intro) : null,
        h('input', { class: 'input', value, autofocus: true, maxlength: 80, onInput: (e) => { name = e.target.value; } })),
      actions: [{ label: 'Cancel' }, { label: okLabel, kind: 'primary', onClick: () => { ok = true; } }],
      onClose: () => resolve(ok ? name : null),
    });
  });

  const makeReusable = async (b, i) => {
    const name = await askName('Make this section reusable',
      'It moves into your library of reusable sections. Add it to other pages from "Add a section". Editing it anywhere updates every page that uses it.',
      summary(b) || types[b.type].label, 'Make reusable');
    if (name == null) return;
    try {
      const sh = await api.createShared(name, b);
      app.shared = [...(app.shared || []), { ...sh, uses: 1 }];
      blocks[i] = { id: b.id, type: 'shared', data: { ref: sh.id } };
      renderBlocks();
      save();
      toast(`"${sh.name}" is now reusable`);
    } catch (e) { toast(e.message, 'error'); }
  };

  const renameShared = async (sh) => {
    const name = await askName('Rename reusable section', '', sh.name, 'Rename');
    if (name == null) return;
    try {
      await api.updateShared(sh.id, { name });
      sh.name = name.trim() || sh.name;
      renderBlocks();
    } catch (e) { toast(e.message, 'error'); }
  };

  // Fine-tuning for one section: spacing, height, background, width. Every
  // control starts at "Auto", which is whatever the theme does.
  const styleFields = Object.fromEntries((schema.styleFields || []).map((f) => [f.key, f]));
  const styleOpen = new Set();
  const styleDrawer = (b, t, save) => {
    const fields = (t.style || []).map((k) => styleFields[k]).filter(Boolean);
    if (!fields.length) return null;
    b.style ||= {};
    const label = h('span', { class: 'style-summary' });
    const describe = () => {
      const picked = fields.filter((f) => b.style[f.key]).map((f) => f.options.find((o) => o.value === b.style[f.key])?.label);
      reset.hidden = !picked.length;
      if (b.css?.trim()) picked.push('Custom CSS');
      label.textContent = picked.length ? picked.join(', ') : 'Auto';
    };
    const onChange = () => {
      for (const k of Object.keys(b.style)) if (!b.style[k]) delete b.style[k];
      describe();
      save();
    };
    const reset = h('button', { type: 'button', class: 'link-btn', onClick: () => { b.style = {}; renderBlocks(); save(); } }, 'Reset to auto');
    const drawer = h('details', { class: 'style-drawer', open: styleOpen.has(b.id),
      onToggle: () => { drawer.open ? styleOpen.add(b.id) : styleOpen.delete(b.id); } },
      h('summary', {}, icon('palette'), 'Style', label),
      h('div', { class: 'style-fields' }, renderFields(fields, b.style, { ...ctx, onChange }), reset,
        // Power users: CSS for just this section.
        h('details', { class: 'advanced', open: !!b.css },
          h('summary', {}, icon('code'), 'Custom CSS', h('span', { class: 'muted' }, 'advanced')),
          codeArea(b.css || '', 'padding: 4rem;\nh2 { color: crimson; }', (v) => { b.css = v; describe(); save(); }, 4),
          h('div', { class: 'field-help' }, 'Applies only to this section. Plain properties style the section itself; rules like h2 { … } style things inside it.'))));
    describe();
    return drawer;
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
    const insert = (b) => {
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
    };
    const removeShared = async (sh) => {
      const ok = await confirmDialog(
        sh.uses ? `"${sh.name}" is used on ${sh.uses} page${sh.uses > 1 ? 's' : ''}. Each of those pages keeps its own copy, which you can then edit separately.`
          : `"${sh.name}" isn't used on any page.`,
        { okLabel: 'Delete', danger: true, title: 'Delete reusable section?' });
      if (!ok) return;
      dlg.close();
      await app.saver.flush();
      try {
        await api.deleteShared(sh.id);
        await app.refreshShared();
        toast('Reusable section deleted');
        app.openPage(page.id, { reload: true }); // pages that used it now hold copies
      } catch (e) { toast(e.message, 'error'); }
    };
    const reusable = app.shared || [];
    const option = (iconName, title, desc, onClick, extra = null) =>
      h('div', { class: 'block-option-wrap' },
        h('button', { type: 'button', class: 'block-option', onClick },
          h('span', { class: 'block-option-icon' }, icon(iconName)),
          h('span', {}, h('strong', {}, title), h('small', {}, desc))),
        extra);
    const dlg = modal({
      title: 'Add a section',
      wide: true,
      body: h('div', {},
        reusable.length ? [
          h('h3', { class: 'picker-group' }, 'Your reusable sections'),
          h('div', { class: 'block-picker' }, reusable.map((sh) => option('link', sh.name,
            `${types[sh.block.type]?.label || 'Section'} · ${sh.uses ? `on ${sh.uses} page${sh.uses > 1 ? 's' : ''}` : 'not used yet'}`,
            () => insert({ id: uid(), type: 'shared', data: { ref: sh.id } }),
            iconButton('trash', 'Delete from library', (e) => { e.stopPropagation(); removeShared(sh); }, 'danger option-delete')))),
          h('h3', { class: 'picker-group' }, 'New section'),
        ] : null,
        h('div', { class: 'block-picker' }, schema.blocks.filter((t) => !t.hidden).map((t) =>
          option(t.icon, t.label, t.description, () => insert({ id: uid(), type: t.type, data: defaultsFor(t.fields) }))))),
    });
  };

  renderBlocks();
  root.replaceChildren(h('div', { class: 'page-editor' },
    h('div', { class: 'page-header' }, titleInput, pathEl, changeEl, flags, seo),
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
    // change: "changed" | "new" | undefined, compared with the published site.
    setChange(change, everPublished) {
      changeEl.hidden = !everPublished || !change;
      if (changeEl.hidden) return;
      changeEl.replaceChildren(h('span', { class: 'change-dot' }),
        change === 'new' ? 'New page, not published yet' : 'Unpublished changes',
        change === 'changed' ? h('button', { type: 'button', class: 'link-btn', onClick: undoPage }, 'Undo changes to this page') : null);
    },
  };
}

function summary(b) {
  const d = b.data || {};
  const s = d.heading || d.text || d.label || (d.html || '').replace(/<[^>]+>/g, ' ') || '';
  return s.trim().slice(0, 50);
}
