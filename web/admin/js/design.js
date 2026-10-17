// Design (look & feel) and Settings panels. Both edit the site record.
import { h, icon } from './ui.js';
import { renderField, segmented, toggle, codeArea } from './fields.js';

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
        segmented([{ value: 'light', label: 'Light' }, { value: 'dark', label: 'Dark' }], t.mode, (v) => { t.mode = v; save(); })),
      h('div', { class: 'field inline' }, h('span', { class: 'field-label' }, 'Heading size'),
        segmented([{ value: 'small', label: 'Smaller' }, { value: 'normal', label: 'Normal' }, { value: 'large', label: 'Larger' }], t.heading, (v) => { t.heading = v; save(); }))),
    headerSection(site, ctx, save),
    footerSection(site, ctx, save),
    section('Custom CSS',
      h('p', { class: 'field-help' }, 'For power users: CSS added after the theme, on every page, so it can override anything. Leave it empty if you don’t need it.'),
      codeArea(site.customCss || '', '.brand { letter-spacing: 0.05em; }', (v) => { site.customCss = v; save(); }, 5))));
}

const inline = (label, control) => h('div', { class: 'field inline' }, h('span', { class: 'field-label' }, label), control);

function headerSection(site, ctx, save) {
  const hd = site.header;
  return section('Header',
    inline('Layout', segmented([{ value: 'classic', label: 'Logo left' }, { value: 'centered', label: 'Centred' }], hd.layout, (v) => { hd.layout = v; save(); })),
    inline('Background', segmented([{ value: '', label: 'Plain' }, { value: 'soft', label: 'Soft' }, { value: 'brand', label: 'Brand' }, { value: 'contrast', label: 'Contrast' }], hd.background, (v) => { hd.background = v; save(); })),
    inline('Stay at the top when scrolling', toggle(hd.sticky, (v) => { hd.sticky = v; save(); })),
    renderField({ key: 'buttonLabel', kind: 'text', label: 'Button', placeholder: 'e.g. Book now (leave empty for no button)',
      help: 'An eye-catching button on the right of the menu, on every page.' }, hd, ctx),
    renderField({ key: 'buttonLink', kind: 'link', label: 'Button goes to' }, hd, ctx));
}

const SOCIAL = [
  ['instagram', 'Instagram', 'instagram.com/yourname'], ['facebook', 'Facebook', 'facebook.com/yourpage'],
  ['x', 'X (Twitter)', 'x.com/yourname'], ['linkedin', 'LinkedIn', 'linkedin.com/in/yourname'],
  ['youtube', 'YouTube', 'youtube.com/@yourchannel'], ['tiktok', 'TikTok', 'tiktok.com/@yourname'],
  ['github', 'GitHub', 'github.com/yourname'], ['email', 'Email', 'you@example.com'],
];

function footerSection(site, ctx, save) {
  const ft = site.footer;
  ft.links ||= [];
  ft.social ||= {};
  return section('Footer',
    renderField({ key: 'footerText', kind: 'textarea', label: 'Text', placeholder: 'e.g. your address, opening hours or a short note' }, site, ctx),
    renderField({ key: 'links', kind: 'list', label: 'Links', itemLabel: 'Link', max: 12,
      fields: [{ key: 'label', kind: 'text', label: 'Text', placeholder: 'e.g. Privacy' }, { key: 'link', kind: 'link', label: 'Goes to' }] }, ft, ctx),
    h('div', { class: 'field' }, h('span', { class: 'field-label' }, 'Social links'),
      h('div', { class: 'social-fields' }, SOCIAL.map(([key, label, ph]) => h('label', { class: 'social-field' },
        h('span', {}, label),
        h('input', { class: 'input', value: ft.social[key] || '', placeholder: ph,
          onInput: (e) => { ft.social[key] = e.target.value; save(); } })))),
      h('div', { class: 'field-help' }, 'Fill in the ones you use. Each shows as an icon.')),
    inline('Background', segmented([{ value: '', label: 'Subtle' }, { value: 'plain', label: 'Plain' }, { value: 'soft', label: 'Soft' }, { value: 'brand', label: 'Brand' }, { value: 'contrast', label: 'Contrast' }], ft.background, (v) => { ft.background = v; save(); })),
    inline('Show © year and site name', toggle(ft.showCopyright, (v) => { ft.showCopyright = v; save(); })));
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
    section('Publishing',
      renderField({ key: 'baseUrl', kind: 'text', label: 'Website address', placeholder: 'https://www.example.com',
        help: "Where the site will live once it's online. Used for search engines (a sitemap) and link previews on social media. Leave empty if you don't know yet." }, site, ctx),
      renderField({ key: 'language', kind: 'text', label: 'Language code', placeholder: 'en',
        help: 'Helps browsers and screen readers. For example en, de, fr, es, nl or pt-BR.' }, site, ctx))));
}
