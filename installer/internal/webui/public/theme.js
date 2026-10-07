// The theme switch, shared by every page of the web installer. It works the
// way AuroraBoot's web UI does (ui/src/lib/theme.ts and hooks/useTheme.ts):
// three modes, system, light and dark, stored in localStorage under the same
// key, and applied as a class on <html>. It is loaded in <head>, so the class
// is set before the page is drawn and the page never flashes the wrong theme.
//
// It also sets .light when the mode resolves to light, which AuroraBoot does
// not need: style.css follows prefers-color-scheme when no script runs, and
// .light is what turns that off for someone who chose light on a dark system.
(function () {
  'use strict';

  var THEME_KEY = 'auroraboot_theme';
  var DARK_MEDIA_QUERY = '(prefers-color-scheme: dark)';
  var MODES = ['system', 'light', 'dark'];
  var NEXT = { system: 'light', light: 'dark', dark: 'system' };
  var LABELS = { system: 'System theme', light: 'Light theme', dark: 'Dark theme' };

  // storedTheme returns the stored mode, or system when nothing valid is
  // stored or storage cannot be read.
  function storedTheme() {
    try {
      var v = window.localStorage.getItem(THEME_KEY);
      return MODES.indexOf(v) >= 0 ? v : 'system';
    } catch (e) {
      return 'system';
    }
  }

  function storeTheme(m) {
    try {
      window.localStorage.setItem(THEME_KEY, m);
    } catch (e) {
      // Storage blocked: the mode still applies for this page load.
    }
  }

  function systemPrefersDark() {
    try {
      return typeof window.matchMedia === 'function' && window.matchMedia(DARK_MEDIA_QUERY).matches;
    } catch (e) {
      return false;
    }
  }

  function resolveTheme(m) {
    if (m === 'system') return systemPrefersDark() ? 'dark' : 'light';
    return m;
  }

  function applyTheme(m) {
    var dark = resolveTheme(m) === 'dark';
    document.documentElement.classList.toggle('dark', dark);
    document.documentElement.classList.toggle('light', !dark);
  }

  var mode = storedTheme();
  applyTheme(mode);

  // In system mode, follow the OS setting when it changes.
  try {
    var mql = window.matchMedia(DARK_MEDIA_QUERY);
    var onChange = function () { if (mode === 'system') applyTheme('system'); };
    if (mql.addEventListener) mql.addEventListener('change', onChange);
    else if (mql.addListener) mql.addListener(onChange);
  } catch (e) {
    // No matchMedia: system mode stays light.
  }

  // Lucide's monitor, sun and moon, drawn with DOM calls rather than markup.
  var SVG = 'http://www.w3.org/2000/svg';
  var ICONS = {
    system: [['rect', { width: 20, height: 14, x: 2, y: 3, rx: 2 }], ['line', { x1: 8, x2: 16, y1: 21, y2: 21 }], ['line', { x1: 12, x2: 12, y1: 17, y2: 21 }]],
    light: [['circle', { cx: 12, cy: 12, r: 4 }], ['path', { d: 'M12 2v2' }], ['path', { d: 'M12 20v2' }], ['path', { d: 'm4.93 4.93 1.41 1.41' }],
      ['path', { d: 'm17.66 17.66 1.41 1.41' }], ['path', { d: 'M2 12h2' }], ['path', { d: 'M20 12h2' }], ['path', { d: 'm6.34 17.66-1.41 1.41' }],
      ['path', { d: 'm19.07 4.93-1.41 1.41' }]],
    dark: [['path', { d: 'M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z' }]],
  };

  function icon(m) {
    var s = document.createElementNS(SVG, 'svg');
    var attrs = { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 2, 'stroke-linecap': 'round', 'stroke-linejoin': 'round', class: 'icon', 'aria-hidden': 'true' };
    Object.keys(attrs).forEach(function (k) { s.setAttribute(k, attrs[k]); });
    ICONS[m].forEach(function (part) {
      var n = document.createElementNS(SVG, part[0]);
      Object.keys(part[1]).forEach(function (k) { n.setAttribute(k, part[1][k]); });
      s.appendChild(n);
    });
    return s;
  }

  var controls = [];

  function setMode(m) {
    mode = m;
    storeTheme(m);
    applyTheme(m);
    controls.forEach(function (draw) { draw(); });
  }

  // A placeholder <div data-theme-toggle> becomes the three-part control;
  // data-theme-toggle="compact" becomes one button that cycles, as on
  // AuroraBoot's icon rail. Every control is redrawn after a change, so the
  // one a breakpoint hides never shows a stale mode.
  function mount(node) {
    if (node.getAttribute('data-theme-toggle') === 'compact') {
      var b = document.createElement('button');
      b.type = 'button';
      b.className = 'theme-cycle';
      b.addEventListener('click', function () { setMode(NEXT[mode]); });
      node.appendChild(b);
      controls.push(function () {
        b.setAttribute('aria-label', 'Theme: ' + mode);
        b.title = 'Theme: ' + mode;
        b.replaceChildren(icon(mode));
      });
    } else {
      var group = document.createElement('div');
      group.className = 'theme-toggle';
      group.setAttribute('role', 'group');
      group.setAttribute('aria-label', 'Theme');
      var buttons = MODES.map(function (m) {
        var mb = document.createElement('button');
        mb.type = 'button';
        mb.setAttribute('aria-label', LABELS[m]);
        mb.title = LABELS[m];
        mb.appendChild(icon(m));
        mb.addEventListener('click', function () { setMode(m); });
        group.appendChild(mb);
        return mb;
      });
      node.appendChild(group);
      controls.push(function () {
        buttons.forEach(function (mb, i) { mb.setAttribute('aria-pressed', String(MODES[i] === mode)); });
      });
    }
    controls[controls.length - 1]();
  }

  function mountAll() {
    Array.prototype.forEach.call(document.querySelectorAll('[data-theme-toggle]'), mount);
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mountAll);
  else mountAll();
})();
