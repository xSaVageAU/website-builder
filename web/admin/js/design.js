// Design (look & feel) and Settings panels. Both edit the site record.
import { h, icon } from './ui.js';
import { renderField, segmented } from './fields.js';

export const SWATCHES = ['#3b5bdb', '#7048e8', '#0f766e', '#2f9e44', '#c2410c', '#e03131', '#d6336c', '#212529'];

export const FONTS = {
  modern: { label: 'Modern', css: 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif' },
  classic: { label: 'Classic', css: 'Charter, "Sitka Text", Cambria, Georgia, serif' },
  friendly: { label: 'Friendly', css: 'ui-rounded, "Arial Rounded MT", Quicksand, Calibri, sans-serif' },
  editorial: { label: 'Editorial', css: 'Didot, "Bodoni MT", "Noto Serif Display", Georgia, serif' },
  technical: { label: 'Technical', css: 'ui-monospace, "Cascadia Code", Menlo, Consolas, monospace' },
};

export function colorPicker(value, onPick) {
  const el = h('div', { class: 'swatches' });
  const render = (v) => {
    const custom = h('input', { type: 'color', value: v, title: 'Pick any colour',
      onInput: (e) => { onPick(e.target.value); }, onChange: (e) => render(e.target.value) });
    el.replaceChildren(
      ...SWATCHES.map((c) => h('button', { type: 'button', class: 'swatch' + (c === v ? ' on' : ''), style: { background: c }, title: c, 'aria-label': c,
        onClick: () => { render(c); onPick(c); } })),
      h('label', { class: 'swatch custom' + (SWATCHES.includes(v) ? '' : ' on'), title: 'Custom colour', style: SWATCHES.includes(v) ? {} : { background: v } },
        SWATCHES.includes(v) ? icon('plus') : null, custom));
  };
  render(value);
  return el;
}

export function fontPicker(value, onPick) {
  const el = h('div', { class: 'font-cards' });
  const render = (v) => el.replaceChildren(...Object.entries(FONTS).map(([k, f]) =>
    h('button', { type: 'button', class: 'font-card' + (k === v ? ' on' : ''), onClick: () => { render(k); onPick(k); } },
      h('span', { class: 'font-sample', style: { fontFamily: f.css } }, 'Aa'),
      h('span', {}, f.label))));
  render(value);
  return el;
}

function section(title, ...children) {
  return h('section', { class: 'design-section' }, h('h2', {}, title), ...children);
}

export function mountDesign(root, app) {
  const site = app.site;
  const t = site.theme;
  const save = () => app.saveSite();
  const ctx = { onChange: save, pages: () => app.pages };
  root.replaceChildren(h('div', { class: 'panel' },
    h('div', { class: 'panel-head' }, h('h1', {}, 'Design')),
    h('p', { class: 'muted' }, 'Every combination is designed to look good, so feel free to experiment. Changes show up in the preview straight away.'),
    section('Brand',
      renderField({ key: 'name', kind: 'text', label: 'Site name' }, site, { ...ctx, onChange: () => { save(); app.renderTopbar(); } }),
      renderField({ key: 'tagline', kind: 'text', label: 'Tagline', placeholder: 'A few words about the site (optional)' }, site, ctx),
      renderField({ key: 'logo', kind: 'image', label: 'Logo', help: 'Optional. Without a logo, the site name is shown instead.' }, site, ctx)),
    section('Colour', colorPicker(t.primary, (c) => { t.primary = c; save(); })),
    section('Fonts', fontPicker(t.fonts, (f) => { t.fonts = f; save(); })),
    section('Style',
      h('div', { class: 'field inline' }, h('span', { class: 'field-label' }, 'Corners'),
        segmented([{ value: 'rounded', label: 'Round' }, { value: 'soft', label: 'Soft' }, { value: 'sharp', label: 'Sharp' }], t.shape, (v) => { t.shape = v; save(); })),
      h('div', { class: 'field inline' }, h('span', { class: 'field-label' }, 'Spacing'),
        segmented([{ value: 'airy', label: 'Airy' }, { value: 'compact', label: 'Compact' }], t.density, (v) => { t.density = v; save(); })),
      h('div', { class: 'field inline' }, h('span', { class: 'field-label' }, 'Mode'),
        segmented([{ value: 'light', label: 'Light' }, { value: 'dark', label: 'Dark' }], t.mode, (v) => { t.mode = v; save(); })))));
}

export function mountSettings(root, app) {
  const site = app.site;
  const save = () => app.saveSite();
  const ctx = { onChange: save, pages: () => app.pages };

  const home = h('select', { class: 'input', onChange: async () => { site.homePageId = Number(home.value); await app.saveSite(true); app.refreshTree(); } },
    app.pages.filter((p) => !p.parentId).map((p) => h('option', { value: p.id, selected: p.id === site.homePageId }, p.title)));

  root.replaceChildren(h('div', { class: 'panel' },
    h('div', { class: 'panel-head' }, h('h1', {}, 'Settings')),
    section('Pages',
      h('div', { class: 'field' }, h('label', { class: 'field-label' }, 'Home page'), home,
        h('div', { class: 'field-help' }, 'The page visitors see first. Only top-level pages can be the home page.'))),
    section('Footer',
      renderField({ key: 'footerText', kind: 'textarea', label: 'Footer text', placeholder: 'e.g. your address, opening hours or a short note',
        help: 'Shown at the bottom of every page.' }, site, ctx)),
    section('Publishing',
      renderField({ key: 'baseUrl', kind: 'text', label: 'Website address', placeholder: 'https://www.example.com',
        help: "Where the site will live once it's online. Used for search engines (a sitemap) and link previews on social media. Leave empty if you don't know yet." }, site, ctx),
      renderField({ key: 'language', kind: 'text', label: 'Language code', placeholder: 'en',
        help: 'Helps browsers and screen readers. For example en, de, fr, es, nl or pt-BR.' }, site, ctx))));
}
