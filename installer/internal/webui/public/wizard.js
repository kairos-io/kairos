// The web installer's wizard. The steps come from GET /api/wizard, the same
// definition the terminal installer renders, and every answer goes through
// POST /api/step/:id, so validation lives on the server. The browser only
// keeps the answers between calls.
'use strict';

const state = { steps: [], answers: {}, cur: 0, yaml: '', edited: false, confirmed: false, errors: {}, regen: false };
const REVIEW = { id: 'review', title: 'Review and install' };

function el(tag, attrs, ...children) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === false || v == null) continue;
    if (k === 'class') n.className = v;
    else if (k.startsWith('on')) n.addEventListener(k.slice(2), v);
    else n.setAttribute(k, v === true ? '' : v);
  }
  for (const c of children.flat()) if (c != null && c !== false) n.append(c.nodeType ? c : document.createTextNode(String(c)));
  return n;
}

async function api(path, body) {
  const r = await fetch(path, body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || r.statusText);
  return j;
}

// answerFor mirrors the TUI's answerFor: the current answer in the wizard's
// string encoding.
function answerFor(f) {
  const a = state.answers;
  switch (f.id) {
    case 'disk': return a.disk || '';
    case 'username': return a.username || '';
    case 'ssh_keys': return (a.ssh_keys || []).join('\n');
    case 'hostname': return a.hostname || '';
    case 'timezone': return a.timezone || '';
    case 'keymap': return a.keymap || '';
    case 'finish_action': return a.finish_action || f.default || '';
    case 'extensions': return (a.extensions || []).map(e => e.name).join('\n');
  }
  let v = a.provider || {};
  for (const k of f.id.split('.')) v = v && typeof v === 'object' ? v[k] : undefined;
  return v == null ? '' : String(v);
}

// values collects one step's inputs, keyed the way Apply reads them.
const values = {};

// One renderer per wizard.Kind. The Go test fails when a Kind is missing.
const RENDERERS = {
  text: (f) => el('input', { type: 'text', id: 'f-' + f.id, value: values[f.id], placeholder: f.placeholder || '', autocomplete: 'off',
    oninput: e => { values[f.id] = e.target.value; } }),
  password: (f) => el('div', { class: 'row' },
    el('input', { type: 'password', id: 'f-' + f.id, placeholder: state.answers.password_hash ? 'unchanged' : 'password', autocomplete: 'new-password',
      oninput: e => { values[f.id] = e.target.value; } }),
    el('input', { type: 'password', id: 'f-' + f.id + '_confirm', 'aria-label': 'Confirm password', placeholder: 'type it again', autocomplete: 'new-password',
      oninput: e => { values[f.id + '_confirm'] = e.target.value; } })),
  choice: (f) => {
    if (!f.choices || f.choices.length === 0) return el('p', { class: 'fhelp' }, 'Nothing to choose from.');
    // The terminal installer puts "(leave unset)" first on an optional choice,
    // so an unanswered list never submits a value the operator did not pick.
    const canLeave = !f.required && !f.choices.some(c => c.value === '');
    if (f.choices.length > 12) {
      const list = el('datalist', { id: 'dl-' + f.id }, f.choices.map(c => el('option', { value: c.value }, c.label)));
      const input = el('input', { type: 'text', id: 'f-' + f.id, list: 'dl-' + f.id, value: values[f.id], placeholder: canLeave ? 'Leave unset, or type to search' : 'type to search',
        autocomplete: 'off', oninput: e => { values[f.id] = e.target.value; } });
      const clear = () => { values[f.id] = ''; input.value = ''; input.focus(); };
      return el('div', { class: 'row' }, input, list,
        canLeave ? el('button', { type: 'button', class: 'btn ghost', onclick: clear }, 'Leave unset') : null);
    }
    const rows = canLeave ? [{ value: '', label: 'Leave unset', detail: 'keep the image default' }, ...f.choices] : f.choices;
    return el('div', { class: 'choices', role: 'radiogroup', 'aria-labelledby': 'l-' + f.id }, rows.map(c => {
      const on = (values[f.id] || '') === c.value;
      const pick = () => { values[f.id] = c.value; draw(); const n = document.getElementById('c-' + f.id + '-' + c.value); if (n) n.focus(); };
      return el('div', { class: 'choice' + (on ? ' sel' : ''), id: 'c-' + f.id + '-' + c.value, role: 'radio', tabindex: '0', 'aria-checked': String(on), onclick: pick,
        onkeydown: e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); pick(); } } },
        el('input', { type: 'radio', tabindex: '-1', checked: on, 'aria-hidden': 'true' }),
        el('div', {}, el('div', { class: 'cl' }, c.label), c.detail ? el('div', { class: 'cd' }, c.detail) : null), el('span'));
    }));
  },
  multichoice: (f) => {
    if (!f.choices || f.choices.length === 0) return el('p', { class: 'fhelp' }, 'Nothing to choose from.');
    const picked = new Set((values[f.id] || '').split('\n').filter(Boolean));
    return el('div', { class: 'choices' }, f.choices.map(c => {
      const on = picked.has(c.value);
      const toggle = () => { on ? picked.delete(c.value) : picked.add(c.value); values[f.id] = [...picked].join('\n'); draw(); };
      return el('div', { class: 'choice' + (on ? ' sel' : ''), role: 'checkbox', tabindex: '0', 'aria-checked': String(on), onclick: toggle,
        onkeydown: e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggle(); } } },
        el('input', { type: 'checkbox', tabindex: '-1', checked: on, 'aria-hidden': 'true' }),
        el('div', {}, el('div', { class: 'cl' }, c.label)), el('span', { class: 'pill' + (c.detail === 'live media' ? ' live' : '') }, c.detail || ''));
    }));
  },
  bool: (f) => el('label', { class: 'checkbox-group' },
    el('input', { type: 'checkbox', id: 'f-' + f.id, checked: values[f.id] === 'true', onchange: e => { values[f.id] = e.target.checked ? 'true' : 'false'; } }), f.label),
  list: (f) => {
    const items = (values[f.id] || '').split('\n').filter(Boolean);
    const input = el('input', { type: 'text', id: 'f-' + f.id + '-new', placeholder: f.placeholder || '', autocomplete: 'off',
      onkeydown: e => { if (e.key === 'Enter') { e.preventDefault(); add(); } } });
    const add = () => { const v = input.value.trim(); if (v) { items.push(v); values[f.id] = items.join('\n'); draw(); } };
    return el('div', {},
      el('div', { class: 'chips' }, items.length ? items.map((k, i) => el('span', { class: 'chip' }, el('span', {}, k),
        el('button', { type: 'button', 'aria-label': 'Remove ' + k, onclick: () => { items.splice(i, 1); values[f.id] = items.join('\n'); draw(); } }, 'x')))
        : el('span', { class: 'fhelp' }, 'Nothing added yet.')),
      el('div', { class: 'row' }, input, el('button', { type: 'button', class: 'btn ghost', onclick: add }, 'Add')));
  },
};

function loadValues(step) {
  for (const k of Object.keys(values)) delete values[k];
  for (const f of step.fields) values[f.id] = f.kind === 'password' ? '' : answerFor(f);
}

function allSteps() { return [...state.steps, REVIEW]; }

function done(s) {
  const a = state.answers;
  return { disk: !!a.disk, user: !!a.username, ssh_keys: (a.ssh_keys || []).length > 0, hostname: !!a.hostname,
    locale: !!(a.timezone || a.keymap), extensions: (a.extensions || []).length > 0, provider: !!a.provider, finish: true }[s.id];
}

function drawRail() {
  const rail = document.getElementById('rail');
  rail.replaceChildren(el('div', { class: 'rail-label' }, 'Steps'), ...allSteps().map((s, i) =>
    el('button', { type: 'button', 'aria-current': i === state.cur ? 'step' : false, onclick: () => go(i) },
      el('span', { class: 'n' }, String(i + 1).padStart(2, '0')), el('span', {}, s.title),
      s !== REVIEW && done(s) ? el('span', { class: 'done', 'aria-label': 'set' }, '\u2713') : el('span'))));
}

function drawStep(step) {
  const main = document.getElementById('main');
  const fields = step.fields.map(f => el('div', { class: 'field' },
    f.kind === 'bool' ? null : el('label', { for: 'f-' + f.id, id: 'l-' + f.id }, f.label),
    f.help ? el('div', { class: 'fhelp' }, f.help) : null,
    RENDERERS[f.kind](f),
    state.errors[f.id] ? el('div', { class: 'err', role: 'alert' }, state.errors[f.id]) : null));
  // replaceChildren turns a null into the text "null", so drop the empty slots.
  main.replaceChildren(...[
    el('div', {}, el('h1', {}, step.title), step.help ? el('p', { class: 'help' }, step.help) : null),
    step.notice ? el('div', { class: 'banner' }, step.notice) : null,
    state.errors[''] ? el('div', { class: 'banner', role: 'alert' }, state.errors['']) : null,
    ...fields,
    el('div', { class: 'actions' },
      el('button', { type: 'button', class: 'btn ghost', disabled: state.cur === 0, onclick: () => go(state.cur - 1) }, 'Back'),
      el('div', { class: 'row' },
        step.optional ? el('button', { type: 'button', class: 'btn ghost', onclick: () => { state.errors = {}; go(state.cur + 1, true); } }, 'Skip') : null,
        el('button', { type: 'button', class: 'btn', id: 'next', onclick: () => go(state.cur + 1) }, 'Next'))),
  ].filter(Boolean));
}

async function validate() {
  const r = await api('/validate-json', { cloud_config: state.yaml }).catch(() => null);
  const banner = document.getElementById('valid');
  if (!banner) return;
  const msg = r ? r.error : '';
  banner.className = 'banner' + (msg ? '' : ' ok');
  banner.textContent = msg ? 'Schema warning: ' + msg.replace(/^.*?#/, '') : 'Valid cloud-config';
}

function drawReview() {
  const a = state.answers, main = document.getElementById('main');
  const area = el('textarea', { id: 'yaml', spellcheck: 'false', 'aria-label': 'cloud-config',
    oninput: e => { state.yaml = e.target.value; state.edited = true; editedTag.hidden = false; clearTimeout(drawReview.t); drawReview.t = setTimeout(validate, 400); } });
  area.value = state.yaml;
  const editedTag = el('span', { class: 'edited' }, 'Edited by hand');
  editedTag.hidden = !state.edited;
  const regenerate = async () => {
    if (state.edited && !state.regen) { state.regen = true; return draw(); }
    state.regen = false; state.edited = false; state.yaml = (await api('/api/render', { answers: a })).cloud_config; draw();
  };
  const disk = a.disk || '(no disk)';
  const confirmText = ((state.steps[0] && state.steps[0].fields[0].confirm) || 'Everything on {value} will be erased.').replace('{value}', disk);
  main.replaceChildren(
    el('div', {}, el('h1', {}, 'Review and install'),
      el('p', { class: 'help' }, 'This is the configuration the install runs with, built from your answers. Edit it to add anything the steps do not ask for.')),
    el('div', { class: 'review' },
      el('div', { class: 'editor' },
        el('div', { class: 'editor-head' }, el('strong', {}, 'cloud-config'),
          el('div', { class: 'row' }, editedTag, el('button', { type: 'button', class: 'btn ghost', onclick: regenerate }, 'Regenerate from answers'))),
        state.regen ? el('div', { class: 'inline-warn' }, 'Regenerating replaces your edits with the configuration built from the answers.',
          el('div', { class: 'row' }, el('button', { type: 'button', class: 'btn', onclick: regenerate }, 'Replace my edits'),
            el('button', { type: 'button', class: 'btn ghost', onclick: () => { state.regen = false; draw(); } }, 'Keep editing'))) : null,
        area, el('div', { class: 'banner ok', id: 'valid' }, 'Valid cloud-config')),
      el('aside', { class: 'side' },
        el('dl', { class: 'sumlist' },
          ...[['Disk', disk], ['User', a.username || 'none'], ['Hostname', a.hostname || 'generated'],
              ['Timezone, keyboard', (a.timezone || 'unset') + ', ' + (a.keymap || 'unset')],
              ['Extensions', (a.extensions || []).map(e => e.name.split('/').pop().replace('.sysext.raw', '')).join(', ') || 'none'],
              ['When done', a.finish_action || 'nothing']].map(([k, v]) => el('div', {}, el('dt', {}, k), el('dd', {}, v)))),
        el('p', { class: 'fhelp' }, 'The disk and the finish action come from your answers, even if the text says otherwise.'),
        el('label', { class: 'confirm' }, el('input', { type: 'checkbox', id: 'confirm', checked: state.confirmed,
          onchange: e => { state.confirmed = e.target.checked; document.getElementById('install').disabled = !state.confirmed || !a.disk; } }), el('span', {}, confirmText)),
        el('button', { type: 'button', class: 'btn', id: 'install', disabled: !state.confirmed || !a.disk, onclick: install }, 'Install to ' + disk),
        el('button', { type: 'button', class: 'btn ghost', onclick: () => go(state.cur - 1) }, 'Back'))));
  validate();
}

async function install() {
  const btn = document.getElementById('install');
  btn.disabled = true; btn.textContent = 'Starting...';
  const r = await fetch('/install', { method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ cloud_config: state.yaml, device: state.answers.disk, finish_action: state.answers.finish_action || '' }), redirect: 'follow' });
  if (r.redirected || r.url.endsWith('progress.html')) { window.location.href = '/progress.html'; return; }
  // A failure to start comes back as the message page; show its text.
  const doc = new DOMParser().parseFromString(await r.text(), 'text/html');
  state.errors[''] = (doc.querySelector('.alert') || doc.body).textContent.trim();
  btn.disabled = false; btn.textContent = 'Install to ' + state.answers.disk;
  document.getElementById('main').prepend(el('div', { class: 'banner', role: 'alert' }, state.errors['']));
}

async function go(i, skip) {
  const s = allSteps()[state.cur];
  if (s !== REVIEW && i > state.cur && !skip) {
    const r = await api('/api/step/' + s.id, { answers: state.answers, values: { ...values } });
    state.errors = {};
    if (r.errors.length) { for (const e of r.errors) state.errors[e.field] = e.message; return draw(); }
    state.answers = r.answers;
  }
  state.errors = {};
  state.cur = Math.max(0, Math.min(allSteps().length - 1, i));
  if (allSteps()[state.cur] === REVIEW && !state.edited) state.yaml = (await api('/api/render', { answers: state.answers })).cloud_config;
  draw();
}

function draw() {
  drawRail();
  const s = allSteps()[state.cur];
  if (s === REVIEW) return drawReview();
  if (!drawStep.loaded || drawStep.loaded !== s.id) { loadValues(s); drawStep.loaded = s.id; }
  drawStep(s);
}

// The finish action is written as '' for nothing; an empty string must still
// be sent so the answers carry the operator's choice.
function answersDefaults() {
  const fin = state.steps.find(s => s.id === 'finish');
  if (fin && state.answers.finish_action === undefined) state.answers.finish_action = fin.fields[0].default || '';
}

api('/api/wizard').then(r => { state.steps = r.steps; answersDefaults(); draw(); })
  .catch(e => document.getElementById('main').replaceChildren(el('div', { class: 'banner', role: 'alert' }, 'The installer steps could not be loaded: ' + e.message)));
