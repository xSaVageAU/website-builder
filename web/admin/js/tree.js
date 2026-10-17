// The sidebar page tree. Its shape *is* the site's menu: nesting a page under
// another makes it a dropdown item, and the order here is the menu order.
import { h, icon, iconButton } from './ui.js';

export function renderTree(root, pages, { currentId, onSelect, onMove, onAdd, onMenu }) {
  const kids = {};
  for (const p of pages) (kids[p.parentId || 0] ||= []).push(p);
  const byId = Object.fromEntries(pages.map((p) => [p.id, p]));
  let dragId = null;

  const isInside = (id, ancestor) => {
    for (let p = byId[id]; p; p = byId[p.parentId]) if (p.id === ancestor) return true;
    return false;
  };

  const clear = () => root.querySelectorAll('.drop-before, .drop-after, .drop-inside').forEach((el) =>
    el.classList.remove('drop-before', 'drop-after', 'drop-inside'));

  const zone = (e, row) => {
    const r = row.getBoundingClientRect();
    const y = (e.clientY - r.top) / r.height;
    return y < 0.28 ? 'before' : y > 0.72 ? 'after' : 'inside';
  };

  const row = (p) => {
    const el = h('div', {
      class: 'tree-row' + (p.id === currentId ? ' current' : '') + (p.live ? '' : ' draft'),
      draggable: 'true',
      tabindex: '0',
      role: 'treeitem',
      'aria-selected': String(p.id === currentId),
      onClick: () => onSelect(p.id),
      onKeydown: (e) => { if (e.key === 'Enter') onSelect(p.id); },
      onDragstart: (e) => { dragId = p.id; e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', String(p.id)); el.classList.add('dragging'); },
      onDragend: () => { dragId = null; el.classList.remove('dragging'); clear(); },
      onDragover: (e) => {
        if (dragId == null || isInside(p.id, dragId)) return;
        e.preventDefault();
        clear();
        el.classList.add('drop-' + zone(e, el));
      },
      onDragleave: () => el.classList.remove('drop-before', 'drop-after', 'drop-inside'),
      onDrop: (e) => {
        if (dragId == null || isInside(p.id, dragId)) return;
        e.preventDefault();
        const z = zone(e, el);
        clear();
        const parent = z === 'inside' ? p.id : p.parentId || 0;
        const siblings = (kids[parent] || []).filter((s) => s.id !== dragId);
        let index = siblings.length;
        if (z !== 'inside') index = siblings.findIndex((s) => s.id === p.id) + (z === 'after' ? 1 : 0);
        onMove(dragId, parent, index);
      },
    },
      icon('grip', 'grip'),
      icon(p.isHome ? 'home' : 'page', 'page-icon'),
      h('span', { class: 'tree-title' }, p.title),
      p.change ? h('span', { class: 'change-dot', title: p.change === 'new' ? 'New, not published yet' : 'Unpublished changes' }) : null,
      !p.published ? h('span', { class: 'badge' }, 'Draft') : !p.live ? h('span', { class: 'badge' }, 'Hidden') : null,
      p.published && !p.showInMenu ? h('span', { class: 'menu-off', title: 'Not shown in the menu' }, icon('eyeOff')) : null,
      h('span', { class: 'tree-actions' },
        iconButton('plus', 'Add a subpage', (e) => { e.stopPropagation(); onAdd(p.id); }),
        iconButton('more', 'Page options', (e) => { e.stopPropagation(); onMenu(p, e.currentTarget); })));
    return el;
  };

  const branch = (parent) => {
    const list = kids[parent];
    if (!list) return null;
    return h('ul', { role: parent ? 'group' : 'tree' }, list.map((p) => h('li', {}, row(p), branch(p.id))));
  };

  // Dropping below the last page moves it to the end of the top level.
  const tail = h('div', { class: 'tree-tail',
    onDragover: (e) => { if (dragId != null) { e.preventDefault(); tail.classList.add('active'); } },
    onDragleave: () => tail.classList.remove('active'),
    onDrop: (e) => {
      e.preventDefault();
      tail.classList.remove('active');
      onMove(dragId, 0, (kids[0] || []).filter((s) => s.id !== dragId).length);
    } });

  root.replaceChildren(branch(0) || h('p', { class: 'muted pad' }, 'No pages yet.'), tail,
    h('p', { class: 'tree-hint' }, 'Drag pages to reorder them. Drop a page onto another to make it a dropdown item.'));
}
