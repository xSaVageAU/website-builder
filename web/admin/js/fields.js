// Form widgets, one per field kind in the block schema (internal/blocks).
// Each widget edits data[field.key] in place and calls onChange().
import { h, icon, iconButton, modal, clone, uid, fill } from './ui.js';
import { pickMedia, mediaById, mediaURL, uploadFiles, dropTarget } from './media.js';

// Fresh data for a new block or list item, from the schema's defaults.
export function defaultsFor(fields) {
  const out = {};
  for (const f of fields || []) {
    if (f.default !== undefined) out[f.key] = f.kind === 'list' ? f.default.map((it) => ({ ...defaultsFor(f.fields), ...clone(it) })) : clone(f.default);
    else if (f.kind === 'image') out[f.key] = 0;
    else if (f.kind === 'toggle') out[f.key] = false;
    else if (f.kind === 'choice') out[f.key] = f.options[0].value;
    else if (f.kind === 'list') out[f.key] = Array.from({ length: f.min || 0 }, () => defaultsFor(f.fields));
    else out[f.key] = '';
  }
  return out;
}

export function renderFields(fields, data, ctx) {
  return (fields || []).map((f) => renderField(f, data, ctx));
}

function wrap(field, control, { inline = false } = {}) {
  const id = 'f-' + uid();
  if (control.matches?.('input, textarea, select')) control.id = id;
  return h('div', { class: 'field field-' + field.kind + (inline ? ' inline' : '') },
    field.label ? h('label', { class: 'field-label', for: id }, field.label) : null,
    control,
    field.help ? h('div', { class: 'field-help' }, field.help) : null);
}

export function renderField(f, data, ctx) {
  const set = (v) => { data[f.key] = v; ctx.onChange(); };
  switch (f.kind) {
    case 'text':
      return wrap(f, h('input', { type: 'text', class: 'input' + (f.big ? ' big' : ''), value: data[f.key] ?? '', placeholder: f.placeholder || '',
        onInput: (e) => set(e.target.value) }));
    case 'textarea':
      return wrap(f, autoGrow(h('textarea', { class: 'input' + (f.big ? ' big' : ''), rows: 2, placeholder: f.placeholder || '',
        onInput: (e) => set(e.target.value) }), data[f.key] ?? ''));
    case 'rich':
      return wrap(f, richText(data[f.key] ?? '', f.placeholder, set, ctx));
    case 'image':
      return wrap(f, imageField(data[f.key] || 0, set));
    case 'link':
      return wrap(f, linkField(data[f.key] || '', set, ctx));
    case 'choice':
      return wrap(f, segmented(f.options, data[f.key], set), { inline: true });
    case 'toggle':
      return wrap(f, toggle(!!data[f.key], set), { inline: true });
    case 'list':
      if (f.fields.length <= 2 && f.fields[0].kind === 'image') return wrap(f, imageList(f, data, ctx));
      return wrap(f, listField(f, data, ctx));
  }
  return h('div', {}, `Unknown field ${f.kind}`);
}

function autoGrow(ta, value) {
  ta.value = value;
  const fit = () => { ta.style.height = 'auto'; ta.style.height = ta.scrollHeight + 2 + 'px'; };
  ta.addEventListener('input', fit);
  requestAnimationFrame(fit);
  return ta;
}

export function segmented(options, value, onPick) {
  const el = h('div', { class: 'seg', role: 'radiogroup' });
  const render = (v) => el.replaceChildren(...options.map((o) => h('button', {
    type: 'button', role: 'radio', 'aria-checked': String(o.value === v), class: o.value === v ? 'on' : '',
    onClick: () => { render(o.value); onPick(o.value); },
  }, o.label)));
  render(value);
  return el;
}

export function toggle(value, onPick, label = '') {
  const input = h('input', { type: 'checkbox', checked: value, onChange: () => onPick(input.checked) });
  return h('label', { class: 'switch' }, input, h('span', { class: 'switch-track' }), label ? h('span', {}, label) : null);
}

// ---------- rich text ----------

function richText(html, placeholder, set, ctx) {
  const area = h('div', { class: 'rich input', contenteditable: 'true', 'data-placeholder': placeholder || 'Start writing…', html });
  const sync = () => {
    // An editor that only contains an empty paragraph counts as empty.
    if (area.textContent.trim() === '' && !area.querySelector('img')) area.innerHTML = area.innerHTML.replace(/^(<p><br><\/p>|<br>|<div><br><\/div>)$/, '');
    set(area.innerHTML);
  };
  area.addEventListener('input', sync);
  area.addEventListener('focus', () => document.execCommand('defaultParagraphSeparator', false, 'p'));
  area.addEventListener('paste', (e) => {
    // Pasting from Word or web pages brings a mess of styles; keep the text only.
    e.preventDefault();
    const text = e.clipboardData.getData('text/plain');
    document.execCommand('insertText', false, text);
  });
  const cmd = (name, arg) => (e) => {
    e.preventDefault();
    area.focus();
    document.execCommand(name, false, arg);
    sync();
  };
  const linkBtn = (e) => {
    e.preventDefault();
    const sel = window.getSelection();
    if (!sel.rangeCount || !area.contains(sel.anchorNode)) { area.focus(); return; }
    const range = sel.getRangeAt(0).cloneRange();
    const existing = sel.anchorNode.parentElement?.closest('a')?.getAttribute('href') || '';
    if (range.collapsed && !existing) {
      ctx.toast?.('Select some text first, then click the link button.');
      return;
    }
    let value = existing;
    const restore = () => { area.focus(); sel.removeAllRanges(); sel.addRange(range); };
    modal({
      title: 'Link',
      body: linkField(value, (v) => { value = v; }, ctx, true),
      actions: [
        existing ? { label: 'Remove link', onClick: () => { restore(); document.execCommand('unlink'); sync(); } } : null,
        { label: 'Cancel' },
        { label: 'Apply', kind: 'primary', onClick: () => { restore(); value ? document.execCommand('createLink', false, value) : document.execCommand('unlink'); sync(); } },
      ].filter(Boolean),
    });
  };
  const bar = h('div', { class: 'rich-bar' },
    toolBtn('bold', 'Bold (Ctrl+B)', cmd('bold')),
    toolBtn('italic', 'Italic (Ctrl+I)', cmd('italic')),
    toolBtn('link', 'Link', linkBtn),
    toolBtn('list', 'Bulleted list', cmd('insertUnorderedList')),
    toolBtn('listOl', 'Numbered list', cmd('insertOrderedList')));
  return h('div', { class: 'rich-wrap' }, bar, area);
}

function toolBtn(name, title, onMousedown) {
  // mousedown, not click, so the text selection survives.
  return h('button', { type: 'button', class: 'tool', title, 'aria-label': title, onMousedown }, icon(name));
}

// ---------- links ----------

// Internal links are stored as "page:<id>" so they survive renames and moves.
export function linkField(value, set, ctx, autofocus = false) {
  const pages = ctx.pages();
  const depth = {};
  for (const p of pages) depth[p.id] = p.parentId ? (depth[p.parentId] ?? 0) + 1 : 0;
  const isPage = /^page:\d+$/.test(value);
  const select = h('select', { class: 'input', autofocus },
    h('option', { value: '' }, '— Nowhere yet —'),
    h('optgroup', { label: 'Your pages' }, pages.map((p) =>
      h('option', { value: 'page:' + p.id, selected: value === 'page:' + p.id }, '  '.repeat(depth[p.id]) + p.title))),
    h('option', { value: '__url', selected: value && !isPage }, 'A web address, email or phone…'));
  if (!value) select.value = '';
  const url = h('input', { type: 'text', class: 'input', placeholder: 'e.g. https://example.com or hello@example.com',
    value: isPage ? '' : value, onInput: () => set(url.value.trim()) });
  const showURL = () => { url.hidden = select.value !== '__url'; };
  select.addEventListener('change', () => {
    showURL();
    if (select.value === '__url') { url.focus(); set(url.value.trim()); } else set(select.value);
  });
  showURL();
  return h('div', { class: 'link-field' }, select, url);
}

// ---------- images ----------

function imageField(id, set) {
  const el = h('div', { class: 'image-field' });
  const render = (id) => {
    const m = mediaById(id);
    el.replaceChildren(m
      ? h('div', { class: 'image-chosen' },
          h('img', { src: mediaURL(m, 480), alt: '' }),
          h('div', { class: 'image-actions' },
            h('button', { type: 'button', class: 'btn small', onClick: choose }, 'Change'),
            h('button', { type: 'button', class: 'btn small ghost', onClick: () => { render(0); set(0); } }, 'Remove')))
      : h('button', { type: 'button', class: 'image-empty', onClick: choose }, icon('image'), h('span', {}, 'Choose an image'), h('small', {}, 'or drop one here')));
  };
  const choose = async () => {
    const [picked] = await pickMedia();
    if (picked) { render(picked); set(picked); }
  };
  dropTarget(el, async (files) => {
    const [m] = await uploadFiles([files[0]]);
    if (m) { render(m.id); set(m.id); }
  });
  render(id);
  return el;
}

// A list whose items are just an image (+ description) is shown as a grid.
function imageList(f, data, ctx) {
  const items = (data[f.key] ||= []);
  const imgKey = f.fields[0].key;
  const altKey = f.fields[1]?.key;
  const grid = h('div', { class: 'image-list' });
  const add = async (ids) => {
    for (const id of ids) {
      if (f.max && items.length >= f.max) break;
      items.push({ ...defaultsFor(f.fields), [imgKey]: id });
    }
    render();
    ctx.onChange();
  };
  const render = () => {
    fill(grid,
      ...items.map((it, i) => {
        const m = mediaById(it[imgKey]);
        return h('div', { class: 'image-tile' },
          m ? h('img', { src: mediaURL(m, 300), alt: '' }) : h('div', { class: 'image-missing' }, icon('image')),
          h('div', { class: 'tile-actions' },
            i > 0 ? iconButton('up', 'Move earlier', () => { items.splice(i - 1, 0, items.splice(i, 1)[0]); render(); ctx.onChange(); }) : null,
            altKey ? iconButton('text', 'Describe this photo', () => {
              let v = it[altKey] || '';
              modal({ title: 'Describe this photo', body: h('div', {},
                h('p', { class: 'muted' }, 'A short description helps people using screen readers, and search engines.'),
                h('input', { class: 'input', value: v, autofocus: true, onInput: (e) => { v = e.target.value; } })),
                actions: [{ label: 'Cancel' }, { label: 'Save', kind: 'primary', onClick: () => { it[altKey] = v; ctx.onChange(); } }] });
            }) : null,
            iconButton('trash', 'Remove from gallery', () => { items.splice(i, 1); render(); ctx.onChange(); })));
      }),
      (!f.max || items.length < f.max)
        ? h('button', { type: 'button', class: 'image-tile add', onClick: async () => add(await pickMedia({ multiple: true })) }, icon('plus'), h('span', {}, 'Add photos'))
        : null);
  };
  dropTarget(grid, async (files) => add((await uploadFiles(files)).map((m) => m.id)));
  render();
  return grid;
}

// ---------- repeated groups ----------

function listField(f, data, ctx) {
  const items = (data[f.key] ||= []);
  const el = h('div', { class: 'list-field' });
  const render = () => {
    fill(el,
      ...items.map((it, i) => h('div', { class: 'list-item' },
        h('div', { class: 'list-item-head' },
          h('span', {}, `${f.itemLabel || 'Item'} ${i + 1}`),
          h('span', { class: 'spacer' }),
          i > 0 ? iconButton('up', 'Move up', () => { items.splice(i - 1, 0, items.splice(i, 1)[0]); render(); ctx.onChange(); }) : null,
          i < items.length - 1 ? iconButton('down', 'Move down', () => { items.splice(i + 1, 0, items.splice(i, 1)[0]); render(); ctx.onChange(); }) : null,
          items.length > (f.min || 0) ? iconButton('trash', 'Remove', () => { items.splice(i, 1); render(); ctx.onChange(); }) : null),
        renderFields(f.fields, it, ctx))),
      (!f.max || items.length < f.max)
        ? h('button', { type: 'button', class: 'btn small ghost add-item', onClick: () => { items.push(defaultsFor(f.fields)); render(); ctx.onChange(); } },
            icon('plus'), `Add ${(f.itemLabel || 'item').toLowerCase()}`)
        : null);
  };
  render();
  return el;
}
