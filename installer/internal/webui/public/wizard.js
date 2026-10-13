// The web installer's wizard. The steps come from GET /api/wizard, the same
// definition the terminal installer renders, and every answer goes through
// POST /api/step/:id, so validation lives on the server. The browser only
// keeps the answers between calls.
'use strict';

const state = { steps: [], advancedDisabled: false, answers: {}, cur: 0, yaml: '', edited: false, confirmedDisk: '', errors: {}, regen: false, applied: new Set(), validSeq: 0 };
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

// fill replaces a node's children. replaceChildren on its own turns a null
// into the text "null", so the empty slots of an optional part are dropped.
function fill(node, ...children) {
  node.replaceChildren(...children.flat().filter(c => c != null && c !== false));
}

async function api(path, body) {
  const r = await fetch(path, body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(r.status === 401 ? UNAUTHORIZED : (j.error || r.statusText || 'HTTP ' + r.status));
  return j;
}

const UNAUTHORIZED = 'The installer refused this request. The page needs the installer access token: open the address the installer printed, with its token, and reload.';

// failure puts a readable message where the page shows it and redraws.
function failure(e) {
  state.errors = { '': e && e.message === UNAUTHORIZED ? e.message : 'Could not reach the installer: ' + (e && e.message ? e.message : e) };
  draw();
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
  return providerValue(f.id);
}

// providerValue reads a dot-separated provider section. A gate, a field id
// ending in '#ask', reads yes when the section it gates was written.
function providerValue(id) {
  if (id.endsWith('#ask')) return String(providerValue(id.slice(0, -4)) !== '');
  let v = state.answers.provider || {};
  for (const k of id.split('.')) v = v && typeof v === 'object' ? v[k] : undefined;
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
        canLeave ? el('button', { type: 'button', class: 'btn outline', onclick: clear }, 'Leave unset') : null);
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
        el('button', { type: 'button', 'aria-label': 'Remove ' + k, onclick: () => { items.splice(i, 1); values[f.id] = items.join('\n'); draw(); } }, '\u00d7')))
        : el('span', { class: 'fhelp' }, 'Nothing added yet.')),
      el('div', { class: 'row' }, input, el('button', { type: 'button', class: 'btn outline', onclick: add }, 'Add')));
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
    locale: !!(a.timezone || a.keymap), extensions: (a.extensions || []).length > 0, provider: !!a.provider, finish: state.applied.has('finish') }[s.id];
}

// The step rail, in the navy sidebar. A step's marker is its number, or a
// check once it is set, as in AuroraBoot's wizard shell.
function drawRail() {
  const rail = document.getElementById('rail');
  fill(rail, el('div', { class: 'rail-label' }, 'Steps'), ...allSteps().map((s, i) => {
    const set = s !== REVIEW && done(s), cur = i === state.cur;
    return el('button', { type: 'button', 'aria-current': cur ? 'step' : false, onclick: () => { drawer(false); go(i); } },
      el('span', { class: 'marker' + (set ? ' done' : ''), 'aria-hidden': 'true' }, set && !cur ? '\u2713' : String(i + 1)),
      el('span', { class: 'step-title' }, s.title), set ? el('span', { class: 'sr-only' }, ', set') : null);
  }));
}

// drawer opens or closes the step list below 768px, where it is a drawer
// behind the top bar, as AuroraBoot's sidebar is. While it is open the rest
// of the page is inert, so focus stays in it; Escape, the backdrop and
// choosing a step close it.
function drawer(open, widened) {
  const shell = document.getElementById('shell');
  if (!shell || shell.classList.contains('drawer-open') === open) return;
  shell.classList.toggle('drawer-open', open);
  document.getElementById('backdrop').hidden = !open;
  document.getElementById('menu').setAttribute('aria-expanded', String(open));
  // Everything behind the backdrop, the top bar included, is out of reach.
  for (const id of ['content', 'topbar']) document.getElementById(id).inert = open;
  const side = document.getElementById('sidebar');
  if (open) { side.setAttribute('role', 'dialog'); side.setAttribute('aria-modal', 'true'); side.setAttribute('aria-label', 'Installation steps'); }
  else for (const a of ['role', 'aria-modal', 'aria-label']) side.removeAttribute(a);
  const step = document.querySelector('#rail [aria-current=step]');
  // A window widened past the breakpoint hides the menu button, so focus
  // goes to the current step, which is now in the visible sidebar.
  if (open || widened) (step || document.getElementById('menu-close')).focus();
  else document.getElementById('menu').focus();
}

// pageHeader is AuroraBoot's PageHeader: where the step is, its title and
// what it is for.
function pageHeader(title, help) {
  return el('header', { class: 'page-header' }, el('div', {},
    el('p', { class: 'eyebrow' }, 'Step ' + (state.cur + 1) + ' of ' + allSteps().length),
    el('h1', {}, title), help ? el('p', { class: 'help' }, help) : null));
}

function drawStep(step) {
  const main = document.getElementById('main');
  const fields = step.fields.map(f => el('div', { class: 'field' },
    f.kind === 'bool' ? null : el('label', { for: 'f-' + f.id, id: 'l-' + f.id }, f.label),
    f.help ? el('div', { class: 'fhelp' }, f.help) : null,
    RENDERERS[f.kind](f),
    state.errors[f.id] ? el('div', { class: 'err', role: 'alert' }, state.errors[f.id]) : null));
  fill(main,
    pageHeader(step.title, step.help),
    step.notice ? el('div', { class: 'banner' }, step.notice) : null,
    state.errors[''] ? el('div', { class: 'banner', role: 'alert' }, state.errors['']) : null,
    el('section', { class: 'card form-card' }, ...fields),
    el('div', { class: 'actions' },
      el('button', { type: 'button', class: 'btn outline', disabled: state.cur === 0, onclick: () => go(state.cur - 1) }, 'Back'),
      el('div', { class: 'row' },
        step.optional ? el('button', { type: 'button', class: 'btn outline', onclick: () => { state.errors = {}; go(state.cur + 1, true); } }, 'Skip') : null,
        el('button', { type: 'button', class: 'btn', id: 'next', onclick: () => go(state.cur + 1) }, 'Next'))));
}

async function validate() {
  const seq = ++state.validSeq;
  const banner = document.getElementById('valid');
  if (banner) { banner.className = 'banner'; banner.textContent = 'Checking the configuration...'; }
  let msg = '', failed = false;
  try { msg = (await api('/validate-json', { cloud_config: state.yaml })).error; } catch (e) { failed = true; msg = e.message; }
  // A later edit started a newer check; only its reply may be shown.
  const now = document.getElementById('valid');
  if (seq !== state.validSeq || !now) return;
  now.className = 'banner' + (msg ? '' : ' ok');
  now.textContent = failed ? 'Could not check the configuration: ' + msg : msg ? 'Schema warning: ' + msg.replace(/^.*?#/, '') : 'Valid cloud-config';
}

function drawReview() {
  const a = state.answers, main = document.getElementById('main');
  const area = el('textarea', { id: 'yaml', spellcheck: 'false', 'aria-label': 'cloud-config',
    oninput: e => { state.yaml = e.target.value; state.edited = true; editedTag.hidden = false; clearTimeout(drawReview.t); drawReview.t = setTimeout(validate, 400); } });
  area.value = state.yaml;
  // The branding switch that hides the terminal installer's editor: the text
  // can be read, not changed, and the install runs with what was generated.
  area.readOnly = state.advancedDisabled;
  const editedTag = el('span', { class: 'edited' }, 'Edited by hand');
  editedTag.hidden = !state.edited;
  const regenerate = async () => {
    if (state.edited && !state.regen) { state.regen = true; return draw(); }
    state.regen = false;
    try { state.yaml = (await api('/api/render', { answers: a })).cloud_config; } catch (e) { return failure(e); }
    state.edited = false; state.errors = {}; draw();
  };
  const disk = a.disk || '(no disk)';
  const confirmText = ((state.steps[0] && state.steps[0].fields[0].confirm) || 'Everything on {value} will be erased.').replace('{value}', disk);
  fill(main,
    pageHeader('Review and install', state.advancedDisabled ? 'This is the configuration the install runs with, built from your answers.'
      : 'This is the configuration the install runs with, built from your answers. Edit it to add anything the steps do not ask for.'),
    state.errors[''] ? el('div', { class: 'banner', role: 'alert' }, state.errors['']) : null,
    el('div', { class: 'review' },
      el('section', { class: 'card editor' },
        el('div', { class: 'editor-head' }, el('h2', { class: 'card-title' }, 'cloud-config'),
          el('div', { class: 'row' }, editedTag, state.advancedDisabled ? null : el('button', { type: 'button', class: 'btn ghost', onclick: regenerate }, 'Regenerate from answers'))),
        state.regen ? el('div', { class: 'inline-warn' }, 'Regenerating replaces your edits with the configuration built from the answers.',
          el('div', { class: 'row' }, el('button', { type: 'button', class: 'btn', onclick: regenerate }, 'Replace my edits'),
            el('button', { type: 'button', class: 'btn outline', onclick: () => { state.regen = false; draw(); } }, 'Keep editing'))) : null,
        area, el('div', { class: 'banner ok', id: 'valid' }, 'Valid cloud-config')),
      el('aside', { class: 'card side' },
        el('h2', { class: 'card-title' }, 'Install summary'),
        el('dl', { class: 'sumlist' },
          ...[['Disk', disk], ['User', a.username || 'none'], ['Hostname', a.hostname || 'generated'],
              ['Timezone, keyboard', (a.timezone || 'unset') + ', ' + (a.keymap || 'unset')],
              ['Extensions', (a.extensions || []).map(e => e.name.split('/').pop().replace('.sysext.raw', '')).join(', ') || 'none'],
              ['When done', a.finish_action || 'nothing']].map(([k, v]) => el('div', {}, el('dt', {}, k), el('dd', {}, v)))),
        el('p', { class: 'fhelp' }, 'The disk and the finish action come from your answers, even if the text says otherwise.'),
        el('label', { class: 'confirm' }, el('input', { type: 'checkbox', id: 'confirm', checked: state.confirmedDisk === a.disk && !!a.disk,
          onchange: e => { state.confirmedDisk = e.target.checked ? a.disk : ''; document.getElementById('install').disabled = !e.target.checked || !a.disk; } }), el('span', {}, confirmText)),
        el('button', { type: 'button', class: 'btn lg', id: 'install', disabled: !(state.confirmedDisk === a.disk && a.disk), onclick: install }, 'Install to ' + disk),
        el('button', { type: 'button', class: 'btn outline', onclick: () => go(state.cur - 1) }, 'Back'))));
  validate();
}

async function install() {
  const btn = document.getElementById('install');
  btn.disabled = true; btn.textContent = 'Starting...';
  try {
    const r = await fetch('/install', { method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cloud_config: state.yaml, device: state.answers.disk, finish_action: state.answers.finish_action || '' }), redirect: 'follow' });
    if (r.redirected || r.url.endsWith('progress.html')) { window.location.href = '/progress.html'; return; }
    if (r.status === 401) throw new Error(UNAUTHORIZED);
    // A failure to start comes back as the message page; show its text.
    const doc = new DOMParser().parseFromString(await r.text(), 'text/html');
    const text = ((doc.querySelector('.alert') || doc.body).textContent || '').trim();
    state.errors = { '': text || 'The install did not start (HTTP ' + r.status + ').' };
    draw();
  } catch (e) { failure(e); }
}

async function go(i, skip) {
  const s = allSteps()[state.cur];
  try {
    if (s !== REVIEW && i > state.cur && !skip) {
      // Text typed into a list but never added still counts.
      for (const f of s.fields.filter(f => f.kind === 'list')) {
        const box = document.getElementById('f-' + f.id + '-new');
        const pending = box ? box.value.trim() : '';
        if (pending) values[f.id] = ((values[f.id] || '') + '\n' + pending).trim();
      }
      const r = await api('/api/step/' + s.id, { answers: state.answers, values: { ...values } });
      if (r.errors.length) { state.errors = {}; for (const e of r.errors) state.errors[e.field] = e.message; return draw(); }
      state.answers = r.answers;
      state.applied.add(s.id);
    }
    const to = Math.max(0, Math.min(allSteps().length - 1, i));
    // Render before moving, so a failure leaves the operator on this step.
    if (allSteps()[to] === REVIEW && !state.edited) state.yaml = (await api('/api/render', { answers: state.answers })).cloud_config;
    state.errors = {};
    state.cur = to;
    draw();
  } catch (e) { failure(e); }
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

document.getElementById('menu').addEventListener('click', () => drawer(true));
document.getElementById('menu-close').addEventListener('click', () => drawer(false));
document.getElementById('backdrop').addEventListener('click', () => drawer(false));
document.addEventListener('keydown', e => { if (e.key === 'Escape') drawer(false); });
// Widening the window past the drawer breakpoint leaves no drawer to close.
window.matchMedia('(min-width: 768px)').addEventListener('change', e => { if (e.matches) drawer(false, true); });

api('/api/wizard').then(r => { state.steps = r.steps; state.advancedDisabled = !!r.advanced_disabled; answersDefaults(); draw(); })
  .catch(e => fill(document.getElementById('main'), el('div', { class: 'banner', role: 'alert' }, 'The installer steps could not be loaded: ' + e.message)));
