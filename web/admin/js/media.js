// Media library: the picker dialog, the full-page library view and upload helpers.
import { api } from './api.js';
import { h, icon, modal, toast, confirmDialog } from './ui.js';

let cache = null;

export async function loadMedia(force = false) {
  if (!cache || force) cache = await api.media();
  return cache;
}

export function mediaById(id) {
  return cache?.find((m) => m.id === id) || null;
}

// URL of the smallest rendition at least `min` pixels wide.
export function mediaURL(m, min = 480) {
  if (!m) return '';
  const w = m.sizes.find((s) => s >= min) ?? m.width;
  const file = w === m.width ? `${m.name}${m.ext}` : `${m.name}-${w}${m.ext}`;
  return `/preview/media/${file}`;
}

export async function uploadFiles(files) {
  const list = Array.from(files).filter((f) => f.type.startsWith('image/'));
  if (!list.length) {
    toast('Please choose image files (JPEG, PNG, GIF or WebP).', 'error');
    return [];
  }
  toast(list.length === 1 ? 'Uploading image…' : `Uploading ${list.length} images…`);
  try {
    const res = await api.upload(list);
    for (const p of res.problems) toast(p, 'error');
    await loadMedia(true);
    return res.added;
  } catch (e) {
    toast(e.message, 'error');
    return [];
  }
}

function fileInput(onFiles, multiple = true) {
  const input = h('input', { type: 'file', accept: 'image/*', multiple, hidden: true,
    onChange: () => { onFiles(input.files); input.value = ''; } });
  return input;
}

// Lets an element accept dropped image files.
export function dropTarget(el, onFiles) {
  el.addEventListener('dragover', (e) => {
    if (![...e.dataTransfer.types].includes('Files')) return;
    e.preventDefault();
    el.classList.add('dropping');
  });
  el.addEventListener('dragleave', (e) => { if (!el.contains(e.relatedTarget)) el.classList.remove('dropping'); });
  el.addEventListener('drop', (e) => {
    if (!e.dataTransfer.files.length) return;
    e.preventDefault();
    el.classList.remove('dropping');
    onFiles(e.dataTransfer.files);
  });
}

function thumb(m, { selected, onClick }) {
  return h('button', { type: 'button', class: 'media-thumb' + (selected ? ' selected' : ''), onClick, title: m.originalName },
    h('img', { src: mediaURL(m, 300), alt: '', loading: 'lazy' }),
    selected ? h('span', { class: 'media-check' }, icon('check')) : null);
}

// Opens the picker. Resolves with an array of media ids (empty if cancelled).
export function pickMedia({ multiple = false } = {}) {
  return new Promise(async (resolve) => {
    let chosen = new Set();
    let done = false;
    const grid = h('div', { class: 'media-grid' });
    const useBtn = h('button', { type: 'button', class: 'btn primary', disabled: true }, 'Use selected');

    const finish = (ids) => { done = true; resolve(ids); dlg.close(); };
    const render = (list) => {
      grid.replaceChildren(...(list.length ? list.map((m) => thumb(m, {
        selected: chosen.has(m.id),
        onClick: () => {
          if (!multiple) return finish([m.id]);
          chosen.has(m.id) ? chosen.delete(m.id) : chosen.add(m.id);
          useBtn.disabled = chosen.size === 0;
          useBtn.textContent = chosen.size > 1 ? `Use ${chosen.size} images` : 'Use selected';
          render(cache);
        },
      })) : [h('p', { class: 'muted empty-media' }, 'No images yet. Upload some, or drag them into this window.')]));
    };
    const onFiles = async (files) => {
      const added = await uploadFiles(files);
      if (!added.length) return;
      if (!multiple && added.length === 1) return finish([added[0].id]);
      for (const m of added) chosen.add(m.id);
      useBtn.disabled = false;
      render(cache);
    };
    const input = fileInput(onFiles, true);
    const body = h('div', { class: 'media-picker' },
      h('div', { class: 'media-toolbar' },
        h('button', { type: 'button', class: 'btn primary', onClick: () => input.click() }, icon('upload'), 'Upload images'),
        h('span', { class: 'muted' }, 'or drag photos here'),
        input),
      grid);
    useBtn.addEventListener('click', () => finish([...chosen]));
    const dlg = modal({
      title: multiple ? 'Choose images' : 'Choose an image',
      body, wide: true,
      onClose: () => { if (!done) resolve([]); },
    });
    if (multiple) dlg.box.append(h('div', { class: 'modal-actions' }, useBtn));
    dropTarget(dlg.box, onFiles);
    render(await loadMedia());
  });
}

// Full-page library (sidebar → Media).
export async function mountMediaLibrary(root) {
  const grid = h('div', { class: 'media-grid large' });
  const render = () => {
    grid.replaceChildren(...(cache.length ? cache.map((m) => h('div', { class: 'media-card' },
      h('img', { src: mediaURL(m, 480), alt: '', loading: 'lazy' }),
      h('div', { class: 'media-meta' },
        h('span', { class: 'media-name', title: m.originalName }, m.originalName || m.name),
        h('span', { class: 'muted' }, `${m.width}×${m.height}`),
        h('button', { type: 'button', class: 'icon-btn danger', title: 'Delete image', onClick: () => remove(m) }, icon('trash'))),
    )) : [h('p', { class: 'muted' }, 'Your uploaded images will appear here.')]));
  };
  const remove = async (m) => {
    try {
      await api.deleteMedia(m.id);
    } catch (e) {
      if (e.status !== 409) return toast(e.message, 'error');
      const ok = await confirmDialog(`This image is used on: ${e.data.usedOn.join(', ')}. Delete it anyway? It will disappear from those pages.`,
        { okLabel: 'Delete anyway', danger: true });
      if (!ok) return;
      await api.deleteMedia(m.id, true);
    }
    await loadMedia(true);
    render();
    toast('Image deleted');
  };
  const input = fileInput(async (files) => { await uploadFiles(files); render(); });
  const panel = h('div', { class: 'panel' },
    h('div', { class: 'panel-head' },
      h('h1', {}, 'Media'),
      h('button', { type: 'button', class: 'btn primary', onClick: () => input.click() }, icon('upload'), 'Upload'),
      input),
    h('p', { class: 'muted' }, 'Photos are resized for fast loading and have location data removed automatically.'),
    grid);
  dropTarget(panel, async (files) => { await uploadFiles(files); render(); });
  root.replaceChildren(panel);
  await loadMedia(true);
  render();
}
