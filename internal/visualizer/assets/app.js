/* ═══════════════════════════════════════════════════════════════
   GlassMarble Architecture Explorer — application logic.
   Plain ES2020, no framework. All server data is HTML-escaped
   before rendering; no inline event handlers anywhere.
═══════════════════════════════════════════════════════════════ */
'use strict';

/* ─── helpers ─── */

const $ = (id) => document.getElementById(id);

function esc(v) {
  return String(v ?? '').replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  }[c]));
}

function fmt(n) {
  return typeof n === 'number' && isFinite(n) ? n.toLocaleString('en-US') : '–';
}

/* API errors arrive as text/plain — never blind-parse JSON. */
async function api(path) {
  const res = await fetch(path);
  if (!res.ok) {
    const body = (await res.text()).slice(0, 200);
    throw new Error(`${res.status} ${path}: ${body || res.statusText}`);
  }
  return res.json();
}

function toast(msg, type = 'info') {
  const icons = { info: 'i-info', ok: 'i-check', warn: 'i-warn', err: 'i-warn' };
  const el = document.createElement('div');
  el.className = `toast toast--${type}`;
  el.innerHTML = `<svg aria-hidden="true"><use href="#${icons[type] || 'i-info'}"/></svg><span></span><i class="toast__bar" aria-hidden="true"></i>`;
  el.querySelector('span').textContent = msg;
  $('toasts').appendChild(el);
  /* progress bar counts down, then a soft exit before removal */
  setTimeout(() => el.classList.add('bye'), 4000);
  setTimeout(() => el.remove(), 4350);
}

/* Animated number count-up — one-shot rAF, cancels itself, respects
   reduced motion and non-numeric values. */
const prefersReduced = matchMedia('(prefers-reduced-motion: reduce)').matches;

function countUp(el, target, formatter) {
  el.classList.remove('skel');
  const fmt2 = formatter || ((v) => Math.round(v).toLocaleString('en-US'));
  if (prefersReduced || typeof target !== 'number' || !isFinite(target) || target === 0) {
    el.textContent = typeof target === 'number' && isFinite(target) ? fmt2(target) : '–';
    return;
  }
  const dur = 700;
  const start = performance.now();
  const ease = (t) => 1 - Math.pow(1 - t, 3);
  const step = (now) => {
    const p = Math.min(1, (now - start) / dur);
    el.textContent = fmt2(target * ease(p));
    if (p < 1) requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
}

function debounce(fn, ms) {
  let t;
  return (...args) => { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
}

const theme = () => document.documentElement.getAttribute('data-theme');

/* ─── state ─── */

const state = {
  cy: null,
  rawElements: [],          // untouched /api/graph payload for client-side filtering
  view: 'components',
  layout: 'cose',
  minDegree: 0,
  layersOff: new Set(),
  kindsOff: new Set(),
  overlay: null,            // 'cycles' | 'cutvertices' | 'pagerank' | 'smells' | null
  selectedId: null,
  smells: [],
  smellsSort: { key: 'severity', asc: false },
  components: [],
  compSort: { key: 'instability', asc: false },
  marbles: [],
  currentMarble: null,
  timeline: [],
  mermaidReady: null,       // promise once loading starts
  smoothGraph: false,       // per-element cy transitions, small graphs only
};

const SEV_ORDER = { CRITICAL: 3, HIGH: 2, MEDIUM: 1, LOW: 0 };

/* ─── theme ─── */

function initTheme() {
  let saved = null;
  try { saved = localStorage.getItem('gmb-theme'); } catch (e) { /* private mode */ }
  if (saved === 'light' || saved === 'dark') {
    document.documentElement.setAttribute('data-theme', saved);
  }
  $('themeToggle').addEventListener('click', () => {
    const next = theme() === 'dark' ? 'light' : 'dark';
    /* crossfade every surface for one beat around the swap */
    document.body.classList.add('theming');
    document.documentElement.setAttribute('data-theme', next);
    try { localStorage.setItem('gmb-theme', next); } catch (e) { /* ignore */ }
    if (state.cy) state.cy.style(cyStylesheet());   // restyle in place — never re-init
    if (state.currentMarble) renderMarble(state.currentMarble, true);
    setTimeout(() => document.body.classList.remove('theming'), 420);
  });
}

/* ─── tabs ─── */

const TABS = ['graph', 'intel', 'timeline', 'marbles', 'metrics'];

/* Spring the capsule beneath the active tab (transform-only).
   Measured against the rail's padding box — which is what `left: 0`
   resolves to — so the capsule lands exactly on the tab regardless of
   the rail's border and padding. */
function moveTabIndicator() {
  const active = document.querySelector('.tab[aria-selected="true"]');
  const ind = $('tabsInd');
  if (!active || !ind) return;
  const rail = ind.parentElement;
  const railBox = rail.getBoundingClientRect();
  const tabBox = active.getBoundingClientRect();
  ind.style.width = `${tabBox.width}px`;
  ind.style.transform = `translateX(${tabBox.left - railBox.left - rail.clientLeft}px)`;
}

const PANEL_ANIM = ['panel-enter-r', 'panel-enter-l', 'panel-exit-r', 'panel-exit-l'];
let activeTab = 'graph';

function activateTab(name) {
  const prev = activeTab;
  activeTab = name;
  TABS.forEach((t) => {
    const btn = $(`tabbtn-${t}`);
    const on = t === name;
    btn.setAttribute('aria-selected', String(on));
    btn.tabIndex = on ? 0 : -1;
  });
  moveTabIndicator();

  const incoming = $(`tab-${name}`);
  if (prev !== name && !prefersReduced) {
    /* Liquid switch: the outgoing panel overlays absolutely and flows out
       toward where we came from; the incoming one slides in from the
       direction of travel with a blur-to-sharp settle. */
    const dir = TABS.indexOf(name) > TABS.indexOf(prev) ? 1 : -1;
    const outgoing = $(`tab-${prev}`);
    if (outgoing && !outgoing.hidden) {
      outgoing.classList.remove(...PANEL_ANIM);
      outgoing.classList.add(dir > 0 ? 'panel-exit-l' : 'panel-exit-r');
      setTimeout(() => {
        /* Rapid tab-hopping guard: only hide if it isn't active again. */
        if (activeTab !== prev) {
          outgoing.hidden = true;
          outgoing.classList.remove(...PANEL_ANIM);
        }
      }, 270);
    }
    TABS.forEach((t) => { if (t !== name && t !== prev) $(`tab-${t}`).hidden = true; });
    incoming.hidden = false;
    incoming.classList.remove(...PANEL_ANIM);
    void incoming.offsetWidth;
    incoming.classList.add(dir > 0 ? 'panel-enter-r' : 'panel-enter-l');
  } else {
    TABS.forEach((t) => { $(`tab-${t}`).hidden = t !== name; });
    incoming.classList.remove(...PANEL_ANIM);
  }

  if (name === 'graph' && state.cy) state.cy.resize();
  if (name === 'timeline') revealTimeline();
  if (name === 'marbles') ensureMermaid().catch(() => {});
}

function initTabs() {
  const bar = document.querySelector('.tabs');
  bar.addEventListener('click', (e) => {
    const b = e.target.closest('.tab');
    if (b) activateTab(b.dataset.tab);
  });
  bar.addEventListener('keydown', (e) => {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
    const idx = TABS.indexOf(document.querySelector('.tab[aria-selected="true"]').dataset.tab);
    const next = (idx + (e.key === 'ArrowRight' ? 1 : TABS.length - 1)) % TABS.length;
    activateTab(TABS[next]);
    $(`tabbtn-${TABS[next]}`).focus();
  });
}

/* ─── cytoscape ─── */

const KIND_COLORS = {
  COMPONENT: '#7c5cfb', SERVICE: '#9d6bfb', MODULE: '#5b8def',
  PACKAGE: '#3b82c4', STRUCT: '#22a06b', CLASS: '#22a06b',
  TYPE: '#2fa084', TYPE_ALIAS: '#2fa084', INTERFACE: '#0e9f9f',
  DATABASE: '#cf9236', ENTRYPOINT: '#d64545',
  FUNCTION: '#8a8a96', METHOD: '#8a8a96',
};

function kindColor(kind) {
  return KIND_COLORS[(kind || '').toUpperCase()] || '#8a8a96';
}

function cyStylesheet() {
  const dark = theme() === 'dark';
  const label = dark ? '#c8c8d4' : '#3a3a44';
  /* light-theme edges need a darker base: at the low opacities dense
     graphs use, a pale grey vanishes into the near-white canvas */
  const edge = dark ? '#3c3c4a' : '#8f8fa8';
  const compoundBg = dark ? 'rgba(255,255,255,0.03)' : 'rgba(20,20,40,0.03)';
  const compoundLine = dark ? '#33333f' : '#d8d8e2';
  /* Smooth dim/highlight fades — only on graphs small enough that
     per-element style transitions stay cheap. */
  const smooth = state.smoothGraph && !prefersReduced
    ? { 'transition-property': 'opacity, background-color, line-color', 'transition-duration': '200ms', 'transition-timing-function': 'ease-out' }
    : {};
  return [
    { selector: 'node', style: {
      ...smooth,
      'background-color': 'data(color)',
      'label': 'data(label)',
      'color': label,
      /* depth cues: leaves fade back and shrink their labels, hubs come forward */
      'opacity': 'data(vis)',
      'font-size': 'data(fsize)',
      /* labels materialize as you zoom in — at fit-zoom on a 5k-node view,
         thousands of overlapping labels otherwise wash the canvas out */
      'min-zoomed-font-size': 7,
      'font-family': 'ui-monospace, Consolas, monospace',
      'text-valign': 'bottom',
      'text-margin-y': 4,
      'text-max-width': 110,
      'text-wrap': 'ellipsis',
      'width': 'data(size)',
      'height': 'data(size)',
      'border-width': 0,
    } },
    /* hubs float closest — a soft accent halo lifts them off the board */
    { selector: 'node[depth > 0.72]', style: {
      'underlay-color': '#7c5cfb',
      'underlay-opacity': dark ? 0.18 : 0.13,
      'underlay-padding': 7,
      'underlay-shape': 'ellipse',
    } },
    { selector: 'node[is_entry]', style: { 'border-width': 2, 'border-color': '#d64545' } },
    { selector: ':parent', style: {
      'background-color': compoundBg,
      'border-color': compoundLine,
      'border-width': 1,
      'label': 'data(label)',
      'font-size': 10,
      'text-valign': 'top',
      'text-margin-y': -4,
      'color': label,
      'shape': 'round-rectangle',
    } },
    { selector: 'edge', style: {
      ...smooth,
      'width': 1,
      'line-color': edge,
      'target-arrow-color': edge,
      'target-arrow-shape': 'triangle',
      'arrow-scale': 0.7,
      'curve-style': 'bezier',
      /* edges inherit the depth of their deepest endpoint */
      'opacity': 'data(eop)',
    } },
    { selector: 'edge[?is_cycle]', style: { 'line-style': 'dashed' } },
    { selector: '.dimmed', style: { 'opacity': 0.1 } },
    { selector: 'node.hl', style: { 'border-width': 2, 'border-color': '#7c5cfb', 'opacity': 1 } },
    { selector: 'node:selected', style: { 'border-width': 3, 'border-color': '#7c5cfb', 'opacity': 1 } },
    { selector: 'edge.hl', style: { 'line-color': '#7c5cfb', 'target-arrow-color': '#7c5cfb', 'width': 2, 'opacity': 1 } },
    { selector: '.ov-cycle', style: { 'line-color': '#d64545', 'target-arrow-color': '#d64545', 'width': 2, 'opacity': 1 } },
    { selector: 'node.ov-cut', style: { 'background-color': '#d97036', 'border-width': 3, 'border-color': '#d64545' } },
    { selector: 'node.ov-smell', style: { 'background-color': '#d64545' } },
    { selector: 'node.ov-pr', style: { 'width': 'data(prSize)', 'height': 'data(prSize)', 'background-color': '#7c5cfb' } },
    { selector: '.path-hl', style: {
      'background-color': '#22a06b', 'line-color': '#22a06b',
      'target-arrow-color': '#22a06b', 'width': 2.5, 'opacity': 1,
    } },
  ];
}

function initCy() {
  state.cy = cytoscape({
    container: $('cy'),
    style: cyStylesheet(),
    wheelSensitivity: 0.25,
    maxZoom: 3,
    minZoom: 0.08,
    /* Viewport performance: during pan/zoom render a cached texture and
       skip edges entirely — on the symbols view that is 14k+ edges the
       browser no longer redraws on every frame. */
    textureOnViewport: true,
    hideEdgesOnViewport: true,
    motionBlur: false,
  });

  state.cy.on('tap', 'node', (e) => {
    const n = e.target;
    if (n.isParent()) return;
    selectNode(n.id());
  });
  state.cy.on('tap', (e) => {
    if (e.target === state.cy) clearSelection();
  });

  /* Parallax board: the dotted grid drifts slower than the graph and its
     dots breathe with zoom, so the graph reads as floating above a board.
     rAF-throttled; direct-manipulation feedback, not an autonomous
     animation, so it stays on under prefers-reduced-motion too. */
  const board = document.querySelector('.panel--graph .canvas');
  let gridRaf = false;
  const syncBoard = () => {
    gridRaf = false;
    const pan = state.cy.pan();
    const z = state.cy.zoom();
    const s = Math.max(15, Math.min(34, 22 * Math.pow(z, 0.4)));
    board.style.backgroundPosition = `${pan.x * 0.22}px ${pan.y * 0.22}px`;
    board.style.backgroundSize = `${s}px ${s}px`;
  };
  state.cy.on('pan zoom', () => {
    if (!gridRaf) { gridRaf = true; requestAnimationFrame(syncBoard); }
  });
}

/* Force layout is O(n²)-ish per iteration and runs on the main thread;
   above this size we fall back to a fast layout so the tab never hangs. */
const COSE_NODE_LIMIT = 700;

function layoutOptions(nodeCount) {
  /* Small graphs get an animated settle — nodes glide into place. */
  const animate = !prefersReduced && nodeCount <= 260;
  const common = { animate, animationDuration: 450, animationEasing: 'ease-out', fit: true, padding: 40 };
  let name = state.layout;
  if (name === 'cose' && nodeCount > COSE_NODE_LIMIT) {
    name = 'concentric';
    toast(`${fmt(nodeCount)} nodes — using fast layout (force is capped at ${fmt(COSE_NODE_LIMIT)})`, 'info');
  }
  switch (name) {
    case 'breadthfirst': return { name: 'breadthfirst', directed: true, spacingFactor: 1.1, ...common };
    case 'concentric': return {
      name: 'concentric', minNodeSpacing: 12,
      concentric: (n) => n.data('in_degree') || 0, levelWidth: () => 3, ...common,
    };
    case 'circle': return { name: 'circle', ...common };
    case 'grid': return { name: 'grid', ...common };
    default: return {
      name: 'cose',
      idealEdgeLength: 90,
      nodeRepulsion: 12000,
      nodeOverlap: 12,
      numIter: nodeCount > 300 ? 400 : 800,
      ...common,
    };
  }
}

function runLayout() {
  const n = state.cy.nodes().not(':parent').length;
  state.cy.layout(layoutOptions(n)).run();
}

function decorate(el) {
  if (el.group === 'nodes') {
    const d = el.data;
    d.color = kindColor(d.kind);
    const deg = (d.in_degree || 0) + (d.out_degree || 0);
    d.size = Math.max(14, Math.min(46, 14 + Math.sqrt(deg) * 4));
    /* depth defaults — refined per-view in applyDepth() */
    d.depth = 0.5;
    d.vis = 1;
    d.fsize = 9;
  }
  return el;
}

/* Atmospheric depth: connectivity decides how "close" an element floats.
   Leaves fade toward the board, hubs stay vivid with larger labels; edges
   inherit the depth of their deepest endpoint. Purely data-driven — the
   stylesheet maps vis/fsize/eop, so this costs nothing per frame. */
function applyDepth(nodes, edges) {
  let maxDeg = 1;
  for (const n of nodes) {
    if (n.data.is_compound) continue;
    maxDeg = Math.max(maxDeg, (n.data.in_degree || 0) + (n.data.out_degree || 0));
  }
  /* Dense graphs invert the contrast problem: thousands of translucent
     edges stack into a flat haze that hides the nodes. There, keep nodes
     near-opaque and drop edges to a whisper so density reads as shading
     rather than fog. */
  const dense = nodes.length + edges.length > 1500;
  const nodeFloor = dense ? 0.86 : 0.58;
  const nodeRange = dense ? 0.14 : 0.42;
  const edgeFloor = dense ? 0.05 : 0.3;
  const edgeRange = dense ? 0.14 : 0.55;

  const depthOf = {};
  for (const n of nodes) {
    const d = n.data;
    if (d.is_compound) { d.depth = 0; d.vis = 1; d.fsize = 10; continue; }
    const deg = (d.in_degree || 0) + (d.out_degree || 0);
    const depth = Math.sqrt(deg / maxDeg);
    d.depth = depth;
    d.vis = nodeFloor + nodeRange * depth;
    d.fsize = Math.round(8 + depth * 3);
    depthOf[d.id] = depth;
  }
  for (const e of edges) {
    const d = Math.min(depthOf[e.data.source] ?? 0.5, depthOf[e.data.target] ?? 0.5);
    e.data.eop = edgeFloor + edgeRange * d;
  }
}

function renderGraph() {
  const cy = state.cy;
  const keptNodes = new Set();
  const nodes = [];
  const edges = [];

  for (const el of state.rawElements) {
    if (el.group === 'nodes') {
      const d = el.data;
      const layerOff = d.layer && state.layersOff.has(d.layer);
      const kindOff = d.kind && state.kindsOff.has(d.kind);
      const degLow = !d.is_compound && (d.in_degree || 0) < state.minDegree;
      if (!d.is_compound && (layerOff || kindOff || degLow)) continue;
      keptNodes.add(d.id);
      nodes.push(decorate(el));
    } else {
      edges.push(el);
    }
  }
  /* invariant: never hand cytoscape an edge with a missing endpoint */
  const keptEdges = edges.filter((e) => keptNodes.has(e.data.source) && keptNodes.has(e.data.target));
  /* drop compound parents that lost all children */
  const parentsInUse = new Set(nodes.filter((n) => n.data.parent).map((n) => n.data.parent));
  let finalNodes = nodes.filter((n) => !n.data.is_compound || parentsInUse.has(n.data.id));

  /* Above the force-layout limit the fallback layouts scatter children
     without regard to grouping, so every translucent package box stretches
     across the whole canvas — dozens stacked wash the view out entirely.
     The boxes carry no meaning there: strip compounds (clone the data;
     rawElements must stay intact for later re-renders). */
  const plainCount = finalNodes.filter((n) => !n.data.is_compound).length;
  if (plainCount > COSE_NODE_LIMIT) {
    finalNodes = finalNodes
      .filter((n) => !n.data.is_compound)
      .map((n) => ({ ...n, data: { ...n.data, parent: undefined } }));
  }

  applyDepth(finalNodes, keptEdges);

  cy.startBatch();
  cy.elements().remove();
  cy.add(finalNodes.concat(keptEdges));
  cy.endBatch();

  /* Enable per-element fade transitions only where they stay cheap. */
  const wasSmooth = state.smoothGraph;
  state.smoothGraph = finalNodes.length + keptEdges.length <= 1500;
  if (state.smoothGraph !== wasSmooth) cy.style(cyStylesheet());

  runLayout();

  $('graphEmpty').hidden = finalNodes.length > 0;
  $('hudStatus').textContent =
    `${fmt(finalNodes.filter((n) => !n.data.is_compound).length)} nodes · ${fmt(keptEdges.length)} edges · ${state.view}`;

  clearOverlay();
  if (state.selectedId && !keptNodes.has(state.selectedId)) clearSelection();
}

async function loadGraph() {
  try {
    const els = await api(`/api/graph?view=${encodeURIComponent(state.view)}`);
    state.rawElements = Array.isArray(els) ? els : [];
    buildFilterPills();
    renderGraph();
  } catch (err) {
    $('hudStatus').textContent = 'Failed to load graph';
    toast(`Graph load failed: ${err.message}`, 'err');
  }
}

/* ─── filters ─── */

function buildFilterPills() {
  const layers = new Set();
  const kinds = new Set();
  for (const el of state.rawElements) {
    if (el.group !== 'nodes' || el.data.is_compound) continue;
    if (el.data.layer) layers.add(el.data.layer);
    if (el.data.kind) kinds.add(el.data.kind);
  }
  const mk = (containerId, values, offSet) => {
    const box = $(containerId);
    box.textContent = '';
    [...values].sort().forEach((v) => {
      const b = document.createElement('button');
      b.className = 'pill';
      b.textContent = v.toLowerCase();
      b.setAttribute('aria-pressed', String(!offSet.has(v)));
      b.addEventListener('click', () => {
        if (offSet.has(v)) offSet.delete(v); else offSet.add(v);
        b.setAttribute('aria-pressed', String(!offSet.has(v)));
        renderGraph();
      });
      box.appendChild(b);
    });
    if (!values.size) {
      const p = document.createElement('p');
      p.className = 'acc__none';
      p.textContent = 'n/a for this view';
      box.appendChild(p);
    }
  };
  state.layersOff.clear();
  state.kindsOff.clear();
  mk('layerFilters', layers, state.layersOff);
  mk('kindFilters', kinds, state.kindsOff);
}

/* Custom select: a button + listbox pair. The list is position:fixed so
   the scrollable sidebar cannot clip it; full keyboard support (arrows,
   Home/End, Enter, Escape) and focus returns to the button on close. */
function initSelect(id, onChange) {
  const root = $(id);
  const btn = root.querySelector('.sel__btn');
  const list = root.querySelector('.sel__list');
  const label = root.querySelector('.sel__btn > span');
  const opts = Array.from(list.querySelectorAll('.sel__opt'));
  let focusIdx = Math.max(0, opts.findIndex((o) => o.getAttribute('aria-selected') === 'true'));
  /* Portal the list to <body>: the sidebar clips overflow, and its
     animated sections create stacking contexts that would paint over a
     locally-positioned popup. Fixed positioning makes the DOM location
     irrelevant to layout; aria-controls keeps the pairing. */
  document.body.appendChild(list);

  const place = () => {
    const r = btn.getBoundingClientRect();
    list.style.width = `${r.width}px`;
    list.style.left = `${r.left}px`;
    /* flip above the trigger when there isn't room below */
    const below = window.innerHeight - r.bottom;
    const h = Math.min(list.scrollHeight + 2, 264);
    if (below < h + 12 && r.top > below) list.style.top = `${r.top - h - 6}px`;
    else list.style.top = `${r.bottom + 6}px`;
  };

  const setFocus = (i) => {
    focusIdx = (i + opts.length) % opts.length;
    opts.forEach((o, j) => o.classList.toggle('is-focus', j === focusIdx));
    opts[focusIdx].scrollIntoView({ block: 'nearest' });
  };

  const open = () => {
    list.hidden = false;
    btn.setAttribute('aria-expanded', 'true');
    place();
    setFocus(opts.findIndex((o) => o.getAttribute('aria-selected') === 'true'));
    list.focus();
  };

  const close = (refocus) => {
    if (list.hidden) return;
    list.hidden = true;
    btn.setAttribute('aria-expanded', 'false');
    opts.forEach((o) => o.classList.remove('is-focus'));
    if (refocus) btn.focus();
  };

  const pick = (i) => {
    const opt = opts[i];
    if (!opt) return;
    opts.forEach((o) => o.setAttribute('aria-selected', String(o === opt)));
    label.textContent = opt.textContent;
    close(true);
    onChange(opt.dataset.val);
  };

  btn.addEventListener('click', () => (list.hidden ? open() : close(true)));
  opts.forEach((o, i) => {
    o.addEventListener('click', () => pick(i));
    o.addEventListener('mousemove', () => setFocus(i));
  });
  list.addEventListener('keydown', (e) => {
    switch (e.key) {
      case 'ArrowDown': e.preventDefault(); setFocus(focusIdx + 1); break;
      case 'ArrowUp': e.preventDefault(); setFocus(focusIdx - 1); break;
      case 'Home': e.preventDefault(); setFocus(0); break;
      case 'End': e.preventDefault(); setFocus(opts.length - 1); break;
      case 'Enter': case ' ': e.preventDefault(); pick(focusIdx); break;
      case 'Escape': case 'Tab': close(true); break;
      default: break;
    }
  });
  btn.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(); }
  });
  document.addEventListener('click', (e) => {
    if (!root.contains(e.target) && !list.contains(e.target)) close(false);
  });
  /* fixed positioning would drift on scroll — close instead */
  window.addEventListener('scroll', () => close(false), true);
  window.addEventListener('resize', () => close(false));
}

/* Liquid thumb that glides under the active segment button. */
function moveSegThumb(seg) {
  const on = seg.querySelector('.seg__btn.is-on');
  let thumb = seg.querySelector('.seg__thumb');
  if (!thumb) {
    thumb = document.createElement('span');
    thumb.className = 'seg__thumb';
    thumb.setAttribute('aria-hidden', 'true');
    seg.prepend(thumb);
  }
  if (!on) return;
  /* Measure against the control's padding box (what `left: 0` resolves to)
     so the thumb lands exactly on the button, border and padding included. */
  const segBox = seg.getBoundingClientRect();
  const onBox = on.getBoundingClientRect();
  thumb.style.width = `${onBox.width}px`;
  thumb.style.transform = `translateX(${onBox.left - segBox.left - seg.clientLeft}px)`;
}

function initControls() {
  const seg = $('granularitySeg');
  moveSegThumb(seg);
  window.addEventListener('load', () => moveSegThumb(seg));
  window.addEventListener('resize', debounce(() => moveSegThumb(seg), 120));
  seg.addEventListener('click', (e) => {
    const b = e.target.closest('[data-view]');
    if (!b || b.classList.contains('is-on')) return;
    document.querySelectorAll('#granularitySeg .seg__btn').forEach((x) => x.classList.remove('is-on'));
    b.classList.add('is-on');
    moveSegThumb(seg);
    state.view = b.dataset.view;
    loadGraph();
  });
  initSelect('layoutSel', (val) => {
    state.layout = val;
    runLayout();
  });
  /* Range: --pct drives the gradient fill of the custom track. */
  const range = $('minDegreeRange');
  const syncRange = () => {
    const min = Number(range.min) || 0;
    const span = (Number(range.max) || 100) - min;
    const pct = span > 0 ? ((Number(range.value) - min) / span) * 100 : 0;
    range.style.setProperty('--pct', `${pct}%`);
    $('minDegreeVal').textContent = range.value;
  };
  syncRange();
  range.addEventListener('input', () => {
    state.minDegree = Number(range.value);
    syncRange();
  });
  range.addEventListener('change', renderGraph);

  $('btnFit').addEventListener('click', () => state.cy.fit(undefined, 40));
}

/* ─── selection / inspector ─── */

function selectNode(id) {
  const cy = state.cy;
  const node = cy.getElementById(id);
  if (!node || node.empty()) { toast('Node is not in the current view', 'warn'); return; }

  state.selectedId = id;
  cy.elements().removeClass('hl');
  cy.elements().addClass('dimmed');
  const hood = node.closedNeighborhood();
  hood.removeClass('dimmed');
  node.ancestors().removeClass('dimmed');
  hood.edges().addClass('hl');
  node.addClass('hl');

  const d = node.data();
  $('inspector').hidden = false;
  $('nodeKindBadge').textContent = d.kind || 'NODE';
  $('nodeName').textContent = d.label || d.id;
  $('nodeFile').textContent = d.file ? (d.line ? `${d.file}:${d.line}` : d.file) : '';
  $('nodeLayer').textContent = d.layer || '—';
  $('mInDegree').textContent = fmt(d.in_degree ?? 0);
  $('mOutDegree').textContent = fmt(d.out_degree ?? 0);
  $('mInstability').textContent = typeof d.instability === 'number' ? d.instability.toFixed(2) : '—';
  $('impactResult').hidden = true;
  $('hudSelection').textContent = d.label || d.id;

  const fill = (listId, countId, edgesColl, pick) => {
    const ul = $(listId);
    ul.textContent = '';
    const seen = new Set();
    edgesColl.forEach((e) => {
      const other = pick(e);
      if (other.isParent() || seen.has(other.id())) return;
      seen.add(other.id());
      const li = document.createElement('li');
      const b = document.createElement('button');
      b.textContent = other.data('label') || other.id();
      b.title = other.id();
      b.addEventListener('click', () => focusNode(other.id()));
      li.appendChild(b);
      ul.appendChild(li);
    });
    $(countId).textContent = String(seen.size);
    if (!seen.size) {
      const li = document.createElement('li');
      li.className = 'acc__none';
      li.textContent = 'None in current view';
      ul.appendChild(li);
    }
  };
  fill('inboundList', 'inboundCount', node.incomers('edge'), (e) => e.source());
  fill('outboundList', 'outboundCount', node.outgoers('edge'), (e) => e.target());
}

function clearSelection() {
  state.selectedId = null;
  state.cy.elements().removeClass('dimmed hl');
  $('inspector').hidden = true;
  $('hudSelection').textContent = 'Nothing selected';
}

function focusNode(id) {
  const node = state.cy.getElementById(id);
  if (!node || node.empty()) { toast('Node is not in the current view', 'warn'); return; }
  selectNode(id);
  state.cy.animate({ center: { eles: node }, zoom: Math.max(state.cy.zoom(), 1) }, { duration: 250 });
}

async function runImpact() {
  if (!state.selectedId) return;
  const btn = $('btnImpact');
  btn.disabled = true;
  btn.textContent = 'Analyzing…';
  try {
    const r = await api(`/api/impact?id=${encodeURIComponent(state.selectedId)}`);
    $('impactResult').hidden = false;
    const lvl = (r.risk_level || 'LOW').toUpperCase();
    const badge = $('riskBadge');
    badge.textContent = lvl;
    badge.className = `badge badge--${lvl.toLowerCase()}`;
    const score = Math.max(0, Math.min(100, r.risk_score || 0));
    $('riskScore').textContent = String(score);
    const fillEl = $('riskMeterFill');
    fillEl.style.width = `${score}%`;
    fillEl.style.background =
      score >= 75 ? 'var(--sev-critical)' : score >= 50 ? 'var(--sev-high)' :
      score >= 25 ? 'var(--sev-medium)' : 'var(--ok)';
    $('impDirect').textContent = fmt(r.direct_dependents_count);
    $('impTransitive').textContent = fmt(r.transitive_dependents_count);
    $('impFiles').textContent = fmt(r.total_impacted_files);
    $('impTests').textContent = fmt((r.impacted_test_files || []).length);
    const cmd = r.recommended_test_command || '';
    $('testCmdWrap').hidden = !cmd;
    $('testCmd').textContent = cmd;
  } catch (err) {
    toast(`Impact analysis failed: ${err.message}`, 'err');
  } finally {
    btn.disabled = false;
    btn.textContent = 'Analyze';
  }
}

/* ─── overlays ─── */

function clearOverlay() {
  state.overlay = null;
  document.querySelectorAll('#overlaySeg .tool[data-overlay]').forEach((b) => b.classList.remove('is-on'));
  const cy = state.cy;
  cy.elements().removeClass('dimmed ov-cycle ov-cut ov-smell ov-pr path-hl');
}

async function setOverlay(name) {
  if (state.overlay === name) { clearOverlay(); return; }
  clearOverlay();
  clearSelection();
  const cy = state.cy;
  try {
    if (name === 'cycles') {
      const r = await api('/api/algorithms/cycles');
      const ids = new Set((r.cycles || []).flat());
      if (!ids.size) { toast('No dependency cycles found', 'ok'); return; }
      cy.elements().addClass('dimmed');
      let hit = 0;
      ids.forEach((id) => {
        const n = cy.getElementById(id);
        if (n.nonempty()) { n.removeClass('dimmed'); hit++; }
      });
      cy.edges().forEach((e) => {
        if (ids.has(e.data('source')) && ids.has(e.data('target'))) {
          e.removeClass('dimmed').addClass('ov-cycle');
        }
      });
      if (!hit) {
        cy.elements().removeClass('dimmed');
        toast('Cycle members are symbols — switch to the Symbols view', 'warn');
        return;
      }
      $('hudStatus').textContent = `${fmt(r.count)} cycles · ${fmt(hit)} nodes in view`;
    } else if (name === 'cutvertices') {
      const r = await api('/api/algorithms/cutvertices');
      const pts = r.articulation_points || [];
      if (!pts.length) { toast('No articulation points found', 'ok'); return; }
      cy.elements().addClass('dimmed');
      let hit = 0;
      pts.forEach((p) => {
        const n = cy.getElementById(p.id);
        if (n.nonempty()) { n.removeClass('dimmed').addClass('ov-cut'); hit++; }
      });
      if (!hit) {
        cy.elements().removeClass('dimmed');
        toast('Cut vertices are symbols — switch to the Symbols view', 'warn');
        return;
      }
      $('hudStatus').textContent = `${fmt(r.count)} cut vertices · ${fmt(hit)} in view`;
    } else if (name === 'pagerank') {
      const r = await api('/api/algorithms/pagerank');
      const ranks = r.ranks || [];
      if (!ranks.length) { toast('No PageRank data', 'warn'); return; }
      const max = ranks[0].score || 1;
      let hit = 0;
      ranks.forEach((p) => {
        const n = cy.getElementById(p.id);
        if (n.nonempty()) {
          n.data('prSize', 16 + (p.score / max) * 44);
          n.addClass('ov-pr');
          hit++;
        }
      });
      if (!hit) { toast('Ranked nodes are symbols — switch to the Symbols view', 'warn'); return; }
      $('hudStatus').textContent = `PageRank · top ${fmt(hit)} in view scaled`;
    } else if (name === 'smells') {
      const ids = new Set();
      state.smells.forEach((s) => (s.nodes || []).forEach((id) => ids.add(id)));
      if (!ids.size) { toast('No smell-affected nodes recorded', 'ok'); return; }
      cy.elements().addClass('dimmed');
      let hit = 0;
      ids.forEach((id) => {
        const n = cy.getElementById(id);
        if (n.nonempty()) { n.removeClass('dimmed').addClass('ov-smell'); hit++; }
      });
      if (!hit) {
        cy.elements().removeClass('dimmed');
        toast('No smell-affected nodes in this view', 'warn');
        return;
      }
      $('hudStatus').textContent = `${fmt(state.smells.length)} smells · ${fmt(hit)} nodes in view`;
    }
    state.overlay = name;
    const btn = document.querySelector(`#overlaySeg .tool[data-overlay="${name}"]`);
    if (btn) btn.classList.add('is-on');
  } catch (err) {
    toast(`Overlay failed: ${err.message}`, 'err');
  }
}

/* ─── path trace ─── */

function initTrace() {
  $('btnPathTrace').addEventListener('click', () => {
    const bar = $('traceBar');
    bar.hidden = !bar.hidden;
    if (!bar.hidden) $('traceSource').focus();
  });
  $('btnCloseTrace').addEventListener('click', () => {
    $('traceBar').hidden = true;
    state.cy.elements().removeClass('dimmed path-hl');
  });
  $('btnRunTrace').addEventListener('click', runTrace);
  ['traceSource', 'traceTarget'].forEach((id) =>
    $(id).addEventListener('keydown', (e) => { if (e.key === 'Enter') runTrace(); }));
}

async function runTrace() {
  const src = $('traceSource').value.trim();
  const dst = $('traceTarget').value.trim();
  if (!src || !dst) { toast('Enter both source and target', 'warn'); return; }
  try {
    const r = await api(`/api/paths?source=${encodeURIComponent(src)}&target=${encodeURIComponent(dst)}`);
    const cy = state.cy;
    cy.elements().removeClass('dimmed path-hl');
    if (!r.found || !Array.isArray(r.path) || !r.path.length) {
      toast('No path found between those symbols', 'warn');
      return;
    }
    cy.elements().addClass('dimmed');
    const onPath = new Set(r.path);
    r.path.forEach((id) => {
      const n = cy.getElementById(id);
      if (n.nonempty()) n.removeClass('dimmed').addClass('path-hl');
    });
    cy.edges().forEach((e) => {
      const si = r.path.indexOf(e.data('source'));
      if (si > -1 && r.path[si + 1] === e.data('target')) {
        e.removeClass('dimmed').addClass('path-hl');
      } else if (onPath.has(e.data('source')) && onPath.has(e.data('target'))) {
        e.removeClass('dimmed');
      }
    });
    $('hudStatus').textContent = `Path: ${r.path.length} hops`;
    toast(`Path found — ${r.path.length} nodes`, 'ok');
  } catch (err) {
    toast(`Path trace failed: ${err.message}`, 'err');
  }
}

/* ─── search ─── */

function initSearch() {
  const input = $('omniSearch');
  const results = $('omniResults');
  let items = [];
  let focus = -1;

  const close = () => {
    results.hidden = true;
    input.setAttribute('aria-expanded', 'false');
    focus = -1;
  };

  const render = (list) => {
    items = list;
    results.textContent = '';
    if (!list.length) {
      const d = document.createElement('div');
      d.className = 'omni__none';
      d.textContent = 'No matches';
      results.appendChild(d);
    }
    list.forEach((r, i) => {
      const b = document.createElement('button');
      b.className = 'omni__item';
      b.setAttribute('role', 'option');
      b.innerHTML = `<span class="k badge">${esc(r.kind || '?')}</span>` +
        `<span class="n">${esc(r.name)}</span><span class="f">${esc(r.file || '')}</span>`;
      b.addEventListener('click', () => { pick(i); });
      results.appendChild(b);
    });
    results.hidden = false;
    input.setAttribute('aria-expanded', 'true');
  };

  const pick = (i) => {
    const r = items[i];
    if (!r) return;
    close();
    input.blur();
    activateTab('graph');
    focusNode(r.id);
  };

  const run = debounce(async () => {
    const q = input.value.trim();
    if (q.length < 2) { close(); return; }
    try {
      render(await api(`/api/search?q=${encodeURIComponent(q)}`));
    } catch (err) {
      close();
    }
  }, 180);

  input.addEventListener('input', run);
  input.addEventListener('keydown', (e) => {
    if (results.hidden) return;
    const opts = results.querySelectorAll('.omni__item');
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      focus = e.key === 'ArrowDown'
        ? Math.min(focus + 1, opts.length - 1)
        : Math.max(focus - 1, 0);
      opts.forEach((o, i) => o.classList.toggle('is-focus', i === focus));
      if (opts[focus]) opts[focus].scrollIntoView({ block: 'nearest' });
    } else if (e.key === 'Enter' && focus > -1) {
      e.preventDefault();
      pick(focus);
    } else if (e.key === 'Escape') {
      close();
      input.blur();
    }
  });
  document.addEventListener('click', (e) => {
    if (!$('omniWrap').contains(e.target)) close();
  });
}

/* ─── status ─── */

async function loadStatus() {
  ['statNodes', 'statEdges', 'statFiles', 'statSmells'].forEach((id) => $(id).classList.add('skel'));
  try {
    const s = await api('/api/status');
    $('statusDot').classList.add('ok');
    const commit = s.commit_hash ? ` @ ${String(s.commit_hash).slice(0, 7)}` : '';
    $('statusText').textContent = `${fmt(s.nodes_count)} nodes${commit}`;
    countUp($('statNodes'), s.nodes_count);
    countUp($('statEdges'), s.edges_count);
    countUp($('statFiles'), s.files_count);
    countUp($('statSmells'), s.smells_count);
  } catch (err) {
    ['statNodes', 'statEdges', 'statFiles', 'statSmells'].forEach((id) => $(id).classList.remove('skel'));
    $('statusDot').classList.add('err');
    $('statusText').textContent = 'offline';
  }
}

/* ─── intelligence ─── */

/* /api/intelligence smells: {kind,title,severity,affected_ids,evidence,suggestion}
   /api/smells fallback:      {title,severity,description,nodes}
   `evidence` may be a string OR a structured object — render either. */
const DETAIL_KEYS = ['excerpt', 'summary', 'message', 'description', 'detail', 'reason', 'items'];

function detailText(v) {
  if (v == null) return '';
  if (typeof v === 'string') return v;
  if (Array.isArray(v)) return v.map(detailText).filter(Boolean).join(' · ');
  if (typeof v === 'object') {
    /* Prefer the human-readable field; fall back to primitive entries
       (never dump nested JSON into the table). */
    for (const k of DETAIL_KEYS) {
      if (v[k] != null) {
        const t = detailText(v[k]);
        if (t) return t;
      }
    }
    return Object.entries(v)
      .filter(([, val]) => val != null && typeof val !== 'object')
      .map(([k, val]) => `${k}: ${val}`)
      .join(' · ');
  }
  return String(v);
}

function normalizeSmell(s) {
  return {
    kind: s.kind || '',
    title: s.title || s.kind || 'Smell',
    severity: (s.severity || 'LOW').toUpperCase(),
    detail: detailText(s.evidence) || detailText(s.description),
    suggestion: s.suggestion || '',
    nodes: Array.isArray(s.affected_ids) ? s.affected_ids : (Array.isArray(s.nodes) ? s.nodes : []),
  };
}

async function loadIntelligence() {
  let intel = null;
  try {
    intel = await api('/api/intelligence');
  } catch (err) {
    $('intelEmpty').hidden = false;
    $('metricsEmpty').hidden = false;
    return;
  }
  const patterns = intel.patterns || [];
  const metrics = intel.metrics || {};
  state.components = intel.components || [];

  let smells = (intel.smells || []).map(normalizeSmell);
  if (!smells.length) {
    try { smells = (await api('/api/smells') || []).map(normalizeSmell); } catch (err) { /* keep empty */ }
  }
  state.smells = smells;

  const hasAny = patterns.length || smells.length || Object.keys(metrics).length;
  $('intelEmpty').hidden = !!hasAny;

  /* patterns */
  const pg = $('patternGrid');
  pg.textContent = '';
  patterns.forEach((p) => {
    const conf = Math.round((p.confidence || 0) * 100);
    const card = document.createElement('div');
    card.className = 'card';
    card.innerHTML =
      `<div class="card__head"><span class="card__title">${esc(p.name || p.kind)}</span>` +
      `<span class="card__conf">${conf}%</span></div>` +
      `<p class="card__desc">${esc(p.description || p.evidence || '')}</p>` +
      `<div class="meter"><div class="meter__fill" style="width:${conf}%"></div></div>`;
    pg.appendChild(card);
  });
  if (!patterns.length) pg.innerHTML = '<p class="empty__hint">No patterns detected yet.</p>';

  /* hotspots */
  const hg = $('hotspotsGrid');
  hg.textContent = '';
  (metrics.top_hotspots || []).slice(0, 8).forEach((h) => {
    const card = document.createElement('div');
    card.className = 'card';
    card.innerHTML =
      `<div class="card__head"><span class="card__title">${esc(h.name)}</span>` +
      `<span class="card__conf">pr ${(h.page_rank || 0).toFixed(3)}</span></div>` +
      `<p class="card__desc">fan-in ${fmt(h.fan_in)} · fan-out ${fmt(h.fan_out)}</p>`;
    const btn = document.createElement('button');
    btn.className = 'btn btn--sm';
    btn.style.marginTop = '8px';
    btn.textContent = 'Show in graph';
    btn.addEventListener('click', () => { activateTab('graph'); focusNode(h.node_id); });
    card.appendChild(btn);
    hg.appendChild(card);
  });
  if (!(metrics.top_hotspots || []).length) hg.innerHTML = '<p class="empty__hint">No hotspot data.</p>';

  renderSmellsTable();
  renderMetrics(metrics);
}

function renderSmellsTable() {
  const { key, asc } = state.smellsSort;
  const rows = [...state.smells].sort((a, b) => {
    const va = key === 'severity' ? (SEV_ORDER[a.severity] ?? 0) : String(a[key] || '').toLowerCase();
    const vb = key === 'severity' ? (SEV_ORDER[b.severity] ?? 0) : String(b[key] || '').toLowerCase();
    return (va < vb ? -1 : va > vb ? 1 : 0) * (asc ? 1 : -1);
  });
  const tb = $('smellsBody');
  tb.textContent = '';
  rows.forEach((s) => {
    const tr = document.createElement('tr');
    tr.innerHTML =
      `<td><span class="badge badge--${esc(s.severity.toLowerCase())}">${esc(s.severity)}</span></td>` +
      `<td><strong>${esc(s.title)}</strong>${s.kind ? ` <span class="dim mono">${esc(s.kind)}</span>` : ''}</td>` +
      `<td class="dim">${esc(s.detail)}${s.suggestion ? `<br><em>${esc(s.suggestion)}</em>` : ''}</td>` +
      `<td class="num">${fmt(s.nodes.length)}</td>`;
    tb.appendChild(tr);
  });
  if (!rows.length) {
    tb.innerHTML = '<tr><td colspan="4" class="dim" style="text-align:center;padding:20px">No smells recorded — clean bill of health.</td></tr>';
  }
  document.querySelectorAll('#smellsTable .th-sort').forEach((b) => {
    b.classList.toggle('is-sorted', b.dataset.sort === key);
    b.classList.toggle('asc', b.dataset.sort === key && asc);
  });
}

/* ─── metrics tab ─── */

function renderMetrics(metrics) {
  const kpis = $('metricsKpis');
  kpis.textContent = '';
  const hasMetrics = metrics && Object.keys(metrics).length > 0;
  $('metricsEmpty').hidden = hasMetrics;
  if (!hasMetrics) return;

  const items = [
    { l: 'Nodes', n: metrics.total_nodes },
    { l: 'Edges', n: metrics.total_edges },
    { l: 'Instability', n: metrics.instability, f: (v) => v.toFixed(2), bar: (metrics.instability ?? 0) * 100 },
    { l: 'Graph density', n: metrics.graph_density, f: (v) => v.toFixed(4), bar: Math.min(100, (metrics.graph_density ?? 0) * 1000) },
    { l: 'Cycles', n: metrics.cycle_count },
    { l: 'Dead code nodes', n: metrics.dead_code_node_count },
    { l: 'Layer violations', n: metrics.layer_violation_count },
    { l: 'Max fan-in', n: metrics.max_fan_in },
  ];
  items.forEach((it) => {
    const d = document.createElement('div');
    d.className = 'mkpi';
    d.innerHTML = `<div class="mkpi__l">${esc(it.l)}</div><div class="mkpi__n"></div>` +
      (it.bar != null ? `<div class="meter"><div class="meter__fill" style="width:0"></div></div>` : '');
    kpis.appendChild(d);
    countUp(d.querySelector('.mkpi__n'), it.n ?? 0, it.f);
    const fill = d.querySelector('.meter__fill');
    if (fill) requestAnimationFrame(() => { fill.style.width = `${Math.max(0, Math.min(100, it.bar))}%`; });
  });
  renderComponentsTable();
}

function renderComponentsTable() {
  const { key, asc } = state.compSort;
  const rows = [...state.components].sort((a, b) => {
    const va = typeof a[key] === 'string' ? a[key].toLowerCase() : (a[key] ?? 0);
    const vb = typeof b[key] === 'string' ? b[key].toLowerCase() : (b[key] ?? 0);
    return (va < vb ? -1 : va > vb ? 1 : 0) * (asc ? 1 : -1);
  });
  const tb = $('componentsBody');
  tb.textContent = '';
  rows.forEach((c) => {
    const inst = typeof c.instability === 'number' ? c.instability.toFixed(2) : '—';
    const conf = typeof c.confidence === 'number' ? `${Math.round(c.confidence * 100)}%` : '—';
    const tr = document.createElement('tr');
    tr.innerHTML =
      `<td><strong>${esc(c.name)}</strong><br><span class="dim mono">${esc((c.directories || []).join(', '))}</span></td>` +
      `<td class="dim">${esc(c.kind || '')}</td>` +
      `<td class="num">${fmt(c.ca)}</td><td class="num">${fmt(c.ce)}</td>` +
      `<td class="num">${esc(inst)}</td><td class="num">${esc(conf)}</td>`;
    tb.appendChild(tr);
  });
  if (!rows.length) {
    tb.innerHTML = '<tr><td colspan="6" class="dim" style="text-align:center;padding:20px">No components detected.</td></tr>';
  }
  document.querySelectorAll('#componentsTable .th-sort').forEach((b) => {
    b.classList.toggle('is-sorted', b.dataset.sort === key);
    b.classList.toggle('asc', b.dataset.sort === key && asc);
  });
}

function initTables() {
  $('smellsTable').addEventListener('click', (e) => {
    const b = e.target.closest('.th-sort');
    if (!b) return;
    const k = b.dataset.sort;
    state.smellsSort = { key: k, asc: state.smellsSort.key === k ? !state.smellsSort.asc : k !== 'severity' };
    renderSmellsTable();
  });
  $('componentsTable').addEventListener('click', (e) => {
    const b = e.target.closest('.th-sort');
    if (!b) return;
    const k = b.dataset.sort;
    state.compSort = { key: k, asc: state.compSort.key === k ? !state.compSort.asc : k === 'name' };
    renderComponentsTable();
  });
}

/* ─── timeline ─── */

async function loadTimeline() {
  try {
    const r = await api('/api/timeline');
    state.timeline = Array.isArray(r.timeline) ? r.timeline : [];
  } catch (err) {
    state.timeline = [];
  }
  renderTimeline('ALL');
  $('timelineFilters').addEventListener('click', (e) => {
    const chip = e.target.closest('.chip');
    if (!chip) return;
    document.querySelectorAll('#timelineFilters .chip').forEach((c) => c.classList.toggle('is-on', c === chip));
    renderTimeline(chip.dataset.filter);
  });
}

/* Scroll-reveal for timeline entries — IO adds .vis with a slight stagger. */
let timelineIO = null;

function revealTimeline() {
  if (timelineIO) timelineIO.disconnect();
  timelineIO = new IntersectionObserver((entries) => {
    entries.forEach((e, i) => {
      if (e.isIntersecting) {
        const el = e.target;
        setTimeout(() => el.classList.add('vis'), Math.min(i * 45, 220));
        timelineIO.unobserve(el);
      }
    });
  }, { threshold: 0.08 });
  document.querySelectorAll('.tl-item:not(.vis)').forEach((el) => timelineIO.observe(el));
}

function renderTimeline(filter) {
  const ol = $('timelineStream');
  ol.textContent = '';
  const entries = state.timeline
    .filter((t) => filter === 'ALL' || (t.event_kind || '').toUpperCase() === filter)
    .slice(0, 100);
  $('timelineEmpty').hidden = entries.length > 0;
  entries.forEach((t) => {
    const kind = (t.event_kind || 'EVENT').toUpperCase();
    const date = t.timestamp ? new Date(t.timestamp).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' }) : '';
    const li = document.createElement('li');
    li.className = `tl-item tl-item--${esc(kind)}`;
    const tags = [...(t.components || []), ...(t.tags || [])].slice(0, 6)
      .map((x) => `<span class="tl-item__tag">${esc(x)}</span>`).join('');
    const commit = t.commit_hash ? `<span class="tl-item__tag">${esc(String(t.commit_hash).slice(0, 7))}</span>` : '';
    li.innerHTML =
      `<div class="tl-item__head"><span class="tl-item__title">${esc(t.title || '(untitled)')}</span>` +
      `<span class="tl-item__kind">${esc(kind)}</span><span class="tl-item__date">${esc(date)}</span></div>` +
      (t.description ? `<p class="tl-item__desc">${esc(t.description)}</p>` : '') +
      ((tags || commit) ? `<div class="tl-item__meta">${commit}${tags}</div>` : '');
    ol.appendChild(li);
  });
  revealTimeline();
}

/* ─── diagrams (marbles) ─── */

function ensureMermaid() {
  if (state.mermaidReady) return state.mermaidReady;
  state.mermaidReady = new Promise((resolve, reject) => {
    const s = document.createElement('script');
    s.src = '/assets/mermaid.min.js';
    s.onload = () => resolve();
    s.onerror = () => reject(new Error('failed to load mermaid'));
    document.head.appendChild(s);
  });
  return state.mermaidReady;
}

async function loadMarbles() {
  try {
    state.marbles = await api('/api/marbles') || [];
  } catch (err) {
    state.marbles = [];
  }
  const ul = $('marblesList');
  ul.textContent = '';
  $('marblesEmpty').hidden = state.marbles.length > 0;
  state.marbles.forEach((m) => {
    const li = document.createElement('li');
    const b = document.createElement('button');
    b.className = 'marble';
    b.innerHTML = `<span class="marble__title">${esc(m.title || m.name)}</span>` +
      `<span class="marble__type">${esc(m.type || 'diagram')}</span>`;
    b.addEventListener('click', () => {
      document.querySelectorAll('.marble').forEach((x) => x.classList.remove('is-on'));
      b.classList.add('is-on');
      renderMarble(m);
    });
    li.appendChild(b);
    ul.appendChild(li);
  });
}

let mermaidSeq = 0;

async function renderMarble(m, rethemeOnly = false) {
  state.currentMarble = m;
  $('diagramHead').hidden = false;
  $('diagType').textContent = m.type || 'DIAGRAM';
  $('diagTitle').textContent = m.title || m.name;
  $('diagPlaceholder').hidden = true;
  const stage = $('diagStage');

  try {
    const detail = await api(`/api/marbles?name=${encodeURIComponent(m.name)}`);
    const code = detail.mermaid || '';
    $('diagSource').textContent = code || detail.raw || '';
    if (!code) {
      stage.innerHTML = '<p class="diagram__placeholder">This file has no mermaid block — showing raw source below.</p>';
      $('diagSource').hidden = false;
      return;
    }
    await ensureMermaid();
    window.mermaid.initialize({
      startOnLoad: false,
      securityLevel: 'strict',
      suppressErrorRendering: true,
      theme: theme() === 'dark' ? 'dark' : 'neutral',
    });
    const { svg } = await window.mermaid.render(`gmb-diag-${++mermaidSeq}`, code);
    stage.innerHTML = svg;
  } catch (err) {
    if (!rethemeOnly) toast('Diagram failed to render — showing source', 'warn');
    stage.innerHTML = `<p class="diagram__placeholder">Mermaid could not render this diagram (${esc(err.message)}).<br>The source is shown below.</p>`;
    $('diagSource').hidden = false;
    $('btnDiagSource').setAttribute('aria-pressed', 'true');
  }
}

function initDiagramActions() {
  $('btnDiagSource').addEventListener('click', () => {
    const src = $('diagSource');
    src.hidden = !src.hidden;
    $('btnDiagSource').setAttribute('aria-pressed', String(!src.hidden));
  });
  $('btnDiagFull').addEventListener('click', () => {
    $('diagStage').classList.toggle('is-full');
  });
}

/* ─── keyboard ─── */

function initKeyboard() {
  document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      $('omniSearch').focus();
      $('omniSearch').select();
      return;
    }
    if (e.target.matches('input, textarea, select')) return;
    if (e.key === 'Escape') {
      if ($('diagStage').classList.contains('is-full')) { $('diagStage').classList.remove('is-full'); return; }
      if (!$('traceBar').hidden) { $('traceBar').hidden = true; return; }
      if (state.overlay) { clearOverlay(); return; }
      if (state.selectedId) clearSelection();
      return;
    }
    if (e.key === 'f' && state.cy && !$('tab-graph').hidden) state.cy.fit(undefined, 40);
    const num = Number(e.key);
    if (num >= 1 && num <= TABS.length) activateTab(TABS[num - 1]);
  });
}

/* ─── boot ─── */

document.addEventListener('DOMContentLoaded', () => {
  initTheme();
  initTabs();
  moveTabIndicator();
  window.addEventListener('load', moveTabIndicator);   // re-measure after fonts settle
  window.addEventListener('resize', debounce(moveTabIndicator, 120));
  initCy();
  initControls();
  initTrace();
  initSearch();
  initTables();
  initKeyboard();
  initDiagramActions();

  $('overlaySeg').addEventListener('click', (e) => {
    const b = e.target.closest('[data-overlay]');
    if (b) setOverlay(b.dataset.overlay);
  });
  $('btnImpact').addEventListener('click', runImpact);
  $('btnCloseInspector').addEventListener('click', clearSelection);
  $('btnCopyTestCmd').addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText($('testCmd').textContent);
      toast('Test command copied', 'ok');
    } catch (err) {
      toast('Clipboard unavailable', 'warn');
    }
  });

  loadStatus();
  loadGraph();
  loadIntelligence();
  loadTimeline();
  loadMarbles();
});
