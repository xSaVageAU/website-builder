// First-run setup: three quick questions, then a ready-made site.
import { api } from './api.js';
import { h, icon, toast } from './ui.js';
import { colorPicker, fontPicker } from './design.js';

// onCancel: offered when there are other sites to go back to.
export function runWizard(schema, { onCancel } = {}) {
  return new Promise((resolve) => {
    const state = { name: '', kind: 'business', primary: '', fonts: '' };
    const kinds = schema.starters;
    const root = h('div', { class: 'wizard' });
    document.body.append(root);

    const shell = (step, title, sub, body, { next, back, nextLabel = 'Continue', disabled = false }) => {
      const nextBtn = h('button', { type: 'button', class: 'btn primary big', disabled, onClick: next }, nextLabel);
      root.replaceChildren(h('div', { class: 'wizard-card' },
        h('div', { class: 'wizard-brand' }, h('span', { class: 'logo-dot' }), 'Sitesmith'),
        h('div', { class: 'wizard-steps' }, [1, 2, 3].map((n) => h('span', { class: n <= step ? 'on' : '' }))),
        h('h1', {}, title),
        sub ? h('p', { class: 'muted' }, sub) : null,
        body,
        h('div', { class: 'wizard-actions' },
          back ? h('button', { type: 'button', class: 'btn ghost', onClick: back }, 'Back')
            : onCancel ? h('button', { type: 'button', class: 'btn ghost', onClick: onCancel }, 'Back to my sites') : h('span'),
          nextBtn)));
      return nextBtn;
    };

    const stepName = () => {
      const input = h('input', { class: 'input big', value: state.name, placeholder: 'e.g. Rosa’s Bakery', autofocus: true,
        onInput: () => { state.name = input.value; btn.disabled = !state.name.trim(); },
        onKeydown: (e) => { if (e.key === 'Enter' && state.name.trim()) stepKind(); } });
      const btn = shell(1, "Let's make your website", "What's it called? You can change this any time.", input,
        { next: stepKind, disabled: !state.name.trim() });
      setTimeout(() => input.focus(), 0);
    };

    const stepKind = () => {
      const grid = h('div', { class: 'kind-grid' });
      const render = () => grid.replaceChildren(...kinds.map((k) =>
        h('button', { type: 'button', class: 'kind-card' + (k.id === state.kind ? ' on' : ''),
          onClick: () => { state.kind = k.id; state.primary = ''; state.fonts = ''; render(); } },
          icon(k.icon), h('strong', {}, k.label), h('small', {}, k.description))));
      render();
      shell(2, "What's it for?", "We'll set up the pages you're likely to need, with tips on what to write.", grid,
        { next: stepLook, back: stepName });
    };

    const stepLook = () => {
      const k = kinds.find((x) => x.id === state.kind);
      state.primary ||= k.primary;
      state.fonts ||= k.fonts;
      const body = h('div', { class: 'wizard-look' },
        h('h3', {}, 'Main colour'), colorPicker(state.primary, (c) => { state.primary = c; }),
        h('h3', {}, 'Font style'), fontPicker(state.fonts, (f) => { state.fonts = f; }));
      const btn = shell(3, 'Pick a look', 'Just a starting point — the Design panel has more options.', body, {
        back: stepKind,
        nextLabel: 'Create my site',
        next: async () => {
          btn.disabled = true;
          btn.textContent = 'Creating…';
          try {
            await api.setup({ name: state.name.trim(), kind: state.kind, primary: state.primary, fonts: state.fonts });
            root.remove();
            resolve();
          } catch (e) {
            toast(e.message, 'error');
            btn.disabled = false;
            btn.textContent = 'Create my site';
          }
        },
      });
    };

    stepName();
  });
}
