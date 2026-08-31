// GlassMarble Architecture Intelligence & Visualizer Controller
// High-performance Cytoscape.js & Mermaid.js unified engine

let cy = null;
let rawElements = [];
let selectedNode = null;
let smellsMode = false;
let cutVerticesMode = false;
let cyclesMode = false;
let pageRankMode = false;
let currentLayout = "cose";
let currentGranularity = "components";
let currentSmellsData = [];
let currentSmellsSort = { column: null, asc: true };

// Theme Configuration
const THEME = {
  dark: {
    bg: "#050810",
    nodeStruct: "#06b6d4",
    nodeIface: "#10b981",
    nodeFunc: "#8b5cf6",
    nodeComp: "#7c6af5",
    nodeDb: "#d946ef",
    nodeEntry: "#f5a623",
    nodeDefault: "#49566b",
    nodeText: "#eef2ff",
    compoundBg: "rgba(12, 17, 32, 0.55)",
    compoundBorder: "#1e273d",
    edgeDefault: "rgba(73, 86, 107, 0.5)",
    edgeHighlight: "#7c6af5",
    edgeBlast: "#f43155",
    edgeCycle: "#f43155"
  },
  light: {
    bg: "#eef0f7",
    nodeStruct: "#0891b2",
    nodeIface: "#059669",
    nodeFunc: "#7c3aed",
    nodeComp: "#4f46e5",
    nodeDb: "#c026d3",
    nodeEntry: "#d97706",
    nodeDefault: "#94a3b8",
    nodeText: "#0d1525",
    compoundBg: "rgba(241, 245, 249, 0.6)",
    compoundBorder: "#dde1ed",
    edgeDefault: "rgba(148, 163, 184, 0.55)",
    edgeHighlight: "#7c3aed",
    edgeBlast: "#dc2626",
    edgeCycle: "#e11d48"
  }
};

window.addEventListener("DOMContentLoaded", () => {
  initTabs();
  initCytoscape();
  initMermaid();
  initEventHandlers();
  initFilterPills();
  loadGraphData();
  loadIntelligenceData();
  loadTimelineData();
  loadMarblesCatalog();
  loadMetricsData();
});

// ============================================================
// UTILITY: Animated Number Counter (§10.1 of plan)
// ============================================================
function animateCounter(el, from, to, durationMs) {
  const startTime = performance.now();
  const range = to - from;
  const easeOut = t => 1 - Math.pow(1 - t, 3);
  function update(currentTime) {
    const elapsed = currentTime - startTime;
    const progress = Math.min(elapsed / durationMs, 1);
    el.textContent = Math.round(from + range * easeOut(progress)).toLocaleString();
    if (progress < 1) requestAnimationFrame(update);
  }
  requestAnimationFrame(update);
}

// ============================================================
// UTILITY: Toast Notification System (§10.3 of plan)
// ============================================================
function showToast(message, type = 'success') {
  const container = document.getElementById('toastContainer');
  if (!container) return;
  const toast = document.createElement('div');
  toast.className = `toast toast-${type}`;
  toast.innerHTML = `<span class="toast-icon"></span><span>${message}</span>`;
  container.appendChild(toast);
  requestAnimationFrame(() => toast.classList.add('toast-visible'));
  setTimeout(() => {
    toast.classList.remove('toast-visible');
    setTimeout(() => toast.remove(), 320);
  }, 2500);
}

// ============================================================
// Filter Pill Buttons — sync toggle pills to hidden checkboxes
// ============================================================
function initFilterPills() {
  document.querySelectorAll('#layerFilters .filter-pill-btn').forEach(btn => {
    const value = btn.getAttribute('data-value');
    btn.addEventListener('click', () => {
      btn.classList.toggle('active');
      const cb = document.querySelector(`#layerFilters input[value="${value}"]`);
      if (cb) cb.checked = btn.classList.contains('active');
      renderFilteredGraph();
    });
  });

  document.querySelectorAll('#kindFilters .kind-filter-btn').forEach(btn => {
    const value = btn.getAttribute('data-value');
    btn.addEventListener('click', () => {
      btn.classList.toggle('active');
      const cb = document.querySelector(`#kindFilters input[value="${value}"]`);
      if (cb) cb.checked = btn.classList.contains('active');
      renderFilteredGraph();
    });
  });
}


function initTabs() {
  document.querySelectorAll('.nav-tab').forEach(btn => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.nav-tab').forEach(b => b.classList.remove('active'));
      document.querySelectorAll('.tab-pane').forEach(p => p.classList.remove('active'));

      btn.classList.add('active');
      const targetTab = btn.getAttribute('data-tab');
      document.getElementById(targetTab).classList.add('active');

      // Resize Cytoscape when graph tab becomes visible
      if (targetTab === 'tab-graph' && cy) cy.resize();
    });
  });
}

// Initialize Cytoscape Instance
function initCytoscape() {
  const isLight = document.body.classList.contains("light-theme");
  const t = isLight ? THEME.light : THEME.dark;

  cy = cytoscape({
    container: document.getElementById("cy"),
    boxSelectionEnabled: false,
    autounselectify: false,
    wheelSensitivity: 0.25,
    minZoom: 0.08,
    maxZoom: 4.0,
    style: [
      // Normal Nodes
      {
        selector: 'node[!is_compound]',
        style: {
          'label': 'data(label)',
          'font-family': 'Plus Jakarta Sans, -apple-system, sans-serif',
          'font-size': '11px',
          'font-weight': '600',
          'color': t.nodeText,
          'text-valign': 'bottom',
          'text-margin-y': '6px',
          'text-background-opacity': 0.85,
          'text-background-color': t.bg,
          'text-background-padding': '3px',
          'text-background-shape': 'roundrectangle',
          'width': 'mapData(in_degree, 0, 30, 30, 60)',
          'height': 'mapData(in_degree, 0, 30, 30, 60)',
          'background-color': ele => getNodeColor(ele.data('kind')),
          'border-width': 2,
          'border-color': '#ffffff',
          'border-opacity': 0.45,
          'transition-property': 'background-color, border-color, border-width, opacity',
          'transition-duration': '0.2s'
        }
      },
      // Kind-Specific Shapes
      { selector: 'node[kind = "STRUCT"], node[kind = "CLASS"]', style: { 'shape': 'roundrectangle' } },
      { selector: 'node[kind = "INTERFACE"]', style: { 'shape': 'diamond' } },
      { selector: 'node[kind = "FUNCTION"], node[kind = "METHOD"]', style: { 'shape': 'ellipse' } },
      { selector: 'node[kind = "COMPONENT"], node[kind = "MODULE"]', style: { 'shape': 'round-hexagon', 'width': 46, 'height': 46 } },
      { selector: 'node[kind = "PACKAGE"]', style: { 'shape': 'round-rectangle', 'width': 44, 'height': 44 } },
      { selector: 'node[kind = "DATABASE"]', style: { 'shape': 'barrel' } },
      { selector: 'node[kind = "ENTRYPOINT"]', style: { 'shape': 'star', 'border-color': '#f59e0b', 'border-width': 3 } },

      // Compound Parent Package Containers
      {
        selector: 'node[?is_compound]',
        style: {
          'label': 'data(label)',
          'font-family': 'JetBrains Mono, monospace',
          'font-size': '11px',
          'font-weight': '700',
          'color': t.nodeText,
          'text-valign': 'top',
          'text-halign': 'center',
          'text-margin-y': '-6px',
          'background-color': t.compoundBg,
          'border-width': 1.5,
          'border-style': 'dashed',
          'border-color': t.compoundBorder,
          'shape': 'roundrectangle',
          'padding': '24px'
        }
      },

      // Edges with Directional Arrows
      {
        selector: 'edge',
        style: {
          'width': 2,
          'line-color': t.edgeDefault,
          'target-arrow-color': t.edgeDefault,
          'target-arrow-shape': 'triangle',
          'curve-style': 'bezier',
          'arrow-scale': 1.0,
          'opacity': 0.8,
          'transition-property': 'line-color, target-arrow-color, width, opacity',
          'transition-duration': '0.2s'
        }
      },
      {
        selector: 'edge[?is_cycle]',
        style: {
          'line-color': t.edgeCycle,
          'target-arrow-color': t.edgeCycle,
          'line-style': 'dashed',
          'width': 3.5
        }
      },

      // Interactive States
      {
        selector: 'node:selected',
        style: {
          'border-color': '#ffffff',
          'border-width': 4,
          'border-opacity': 1.0,
          'shadow-blur': 22,
          'shadow-color': '#8b5cf6',
          'shadow-opacity': 0.85
        }
      },
      {
        selector: 'node.highlighted',
        style: {
          'border-color': '#06b6d4',
          'border-width': 3.5,
          'opacity': 1.0
        }
      },
      {
        selector: 'node.blast-target',
        style: {
          'background-color': '#ef4444',
          'border-color': '#ffffff',
          'border-width': 4,
          'shadow-blur': 25,
          'shadow-color': '#ef4444',
          'shadow-opacity': 0.95
        }
      },
      {
        selector: 'node.blast-impacted',
        style: {
          'background-color': '#f97316',
          'border-color': '#ffffff',
          'border-width': 3,
          'opacity': 1.0
        }
      },
      {
        selector: 'node.cut-vertex',
        style: {
          'background-color': '#f59e0b',
          'border-color': '#ffffff',
          'border-width': 4,
          'shadow-blur': 20,
          'shadow-color': '#f59e0b',
          'shadow-opacity': 0.9
        }
      },
      {
        selector: 'node.cycle-node',
        style: {
          'background-color': '#f43f5e',
          'border-color': '#ffffff',
          'border-width': 3.5,
          'shadow-blur': 20,
          'shadow-color': '#f43f5e',
          'shadow-opacity': 0.9
        }
      },
      {
        selector: 'edge.blast-edge',
        style: {
          'line-color': '#ef4444',
          'target-arrow-color': '#ef4444',
          'width': 3.5,
          'opacity': 1.0
        }
      },
      {
        selector: 'edge.path-edge',
        style: {
          'line-color': '#06b6d4',
          'target-arrow-color': '#06b6d4',
          'width': 4,
          'opacity': 1.0
        }
      },
      {
        selector: '.dimmed',
        style: {
          'opacity': 0.12
        }
      }
    ]
  });

  // Node Click Handlers
  cy.on('tap', 'node[!is_compound]', evt => {
    const node = evt.target;
    inspectNode(node.data());
  });

  cy.on('tap', evt => {
    if (evt.target === cy) {
      clearHighlights();
      document.getElementById('hudSelection').textContent = 'Click any node to inspect';
    }
  });
}

function getNodeColor(kind) {
  switch (kind) {
    case 'STRUCT':
    case 'CLASS': return '#06b6d4';
    case 'INTERFACE': return '#10b981';
    case 'FUNCTION':
    case 'METHOD': return '#8b5cf6';
    case 'COMPONENT':
    case 'MODULE': return '#6366f1';
    case 'PACKAGE': return '#3b82f6';
    case 'DATABASE': return '#d946ef';
    case 'ENTRYPOINT': return '#f59e0b';
    default: return '#64748b';
  }
}

// Fetch Graph Data with Granularity (components / packages / symbols)
async function loadGraphData() {
  try {
    const [graphRes, statusRes] = await Promise.all([
      fetch(`/api/graph?view=${currentGranularity}`),
      fetch('/api/status')
    ]);

    rawElements = await graphRes.json();
    const statusData = await statusRes.json();

    // Populate Status Stats with animated counters
    const nodesEl  = document.getElementById('statTotalNodes');
    const edgesEl  = document.getElementById('statTotalEdges');
    const filesEl  = document.getElementById('statTotalFiles');
    const smellsEl = document.getElementById('statTotalSmells');

    animateCounter(nodesEl,  0, statusData.nodes_count  || 0, 700);
    animateCounter(edgesEl,  0, statusData.edges_count  || 0, 700);
    animateCounter(filesEl,  0, statusData.files_count  || 0, 600);
    animateCounter(smellsEl, 0, statusData.smells_count || 0, 500);


    renderFilteredGraph();
    
    const nodeCount = rawElements.filter(e => e.group === 'nodes' && !e.data.is_compound).length;
    const edgeCount = rawElements.filter(e => e.group === 'edges').length;
    document.getElementById('hudStatus').textContent = `Loaded ${nodeCount} nodes & ${edgeCount} relationships (${currentGranularity})`;
  } catch (err) {
    console.error('Failed to load architecture graph:', err);
    document.getElementById('hudStatus').textContent = 'Error loading graph from backend';
  }
}

// Apply Filters & Safely Render Graph (Guarantees Edge Endpoints Exist)
function renderFilteredGraph() {
  if (!cy || !rawElements.length) return;

  const activeLayers = new Set(
    Array.from(document.querySelectorAll('#layerFilters input:checked')).map(cb => cb.value)
  );
  const activeKinds = new Set(
    Array.from(document.querySelectorAll('#kindFilters input:checked')).map(cb => cb.value)
  );
  const minInDegree = parseInt(document.getElementById('minDegreeRange').value, 10) || 0;

  // 1. Filter Nodes First
  const validNodes = [];
  const validNodeIDs = new Set();

  rawElements.forEach(el => {
    if (el.group === 'nodes') {
      if (el.data.is_compound) {
        validNodes.push(el);
        validNodeIDs.add(el.data.id);
        return;
      }
      if (el.data.layer && !activeLayers.has(el.data.layer)) return;
      if (el.data.kind && !activeKinds.has(el.data.kind)) return;
      if (el.data.in_degree < minInDegree) return;

      validNodes.push(el);
      validNodeIDs.add(el.data.id);
    }
  });

  // 2. Filter Edges ensuring BOTH endpoints exist in validNodeIDs
  const validEdges = [];
  rawElements.forEach(el => {
    if (el.group === 'edges') {
      if (validNodeIDs.has(el.data.source) && validNodeIDs.has(el.data.target)) {
        validEdges.push(el);
      }
    }
  });

  cy.elements().remove();
  cy.add([...validNodes, ...validEdges]);
  applyLayout(currentLayout);
}

// Apply Cytoscape Layout
function applyLayout(name) {
  currentLayout = name;
  let layoutOptions = { name: 'cose', animate: true, animationDuration: 500 };

  switch (name) {
    case 'breadthfirst':
      layoutOptions = {
        name: 'breadthfirst',
        directed: true,
        spacingFactor: 1.5,
        avoidOverlap: true,
        animate: true
      };
      break;
    case 'concentric':
      layoutOptions = {
        name: 'concentric',
        concentric: ele => ele.data('in_degree') || 0,
        levelWidth: () => 2,
        avoidOverlap: true,
        animate: true
      };
      break;
    case 'circle':
      layoutOptions = {
        name: 'circle',
        avoidOverlap: true,
        animate: true
      };
      break;
    case 'grid':
      layoutOptions = {
        name: 'grid',
        avoidOverlap: true,
        animate: true
      };
      break;
    default:
      layoutOptions = {
        name: 'cose',
        idealEdgeLength: 70,
        nodeOverlap: 25,
        refresh: 20,
        fit: true,
        padding: 40,
        randomize: false,
        componentSpacing: 100,
        nodeRepulsion: 500000,
        edgeElasticity: 100,
        nestingFactor: 5,
        gravity: 80,
        numIter: 1000,
        initialTemp: 200,
        coolingFactor: 0.95,
        animate: true
      };
  }

  const l = cy.layout(layoutOptions);
  l.run();
}

// Inspect Node Details in Side Drawer
async function inspectNode(data) {
  selectedNode = data;
  document.getElementById('hudSelection').textContent = `Selected: ${data.label} (${data.kind})`;

  const drawer = document.getElementById('inspector');
  drawer.classList.remove('hidden');

  document.getElementById('nodeName').textContent = data.label;
  document.getElementById('nodeKindBadge').textContent = data.kind;
  document.getElementById('nodeKindBadge').style.backgroundColor = getNodeColor(data.kind);

  document.getElementById('nodeFilePath').textContent = data.file ? `${data.file}:${data.line || 1}` : (data.id || '-');
  document.getElementById('nodePackage').textContent = data.parent || (data.kind === 'COMPONENT' ? data.id : 'root');
  document.getElementById('nodeLayer').textContent = (data.layer || 'common').toUpperCase();

  document.getElementById('mInDegree').textContent = data.in_degree || 0;
  document.getElementById('mOutDegree').textContent = data.out_degree || 0;
  document.getElementById('mPageRank').textContent = (data.pagerank || 0).toFixed(3);

  // Inbound & Outbound Lists
  const inEdges = cy.edges(`[target = "${data.id}"]`);
  const outEdges = cy.edges(`[source = "${data.id}"]`);

  const inboundList = document.getElementById('inboundList');
  inboundList.innerHTML = inEdges.length ? inEdges.map(e => `<li onclick="focusNode('${e.data('source')}')"><span>${e.data('source')}</span> <span class="code-text">${e.data('label') || e.data('type')}</span></li>`).join('') : '<li>None</li>';
  document.getElementById('inboundCount').textContent = inEdges.length;

  const outboundList = document.getElementById('outboundList');
  outboundList.innerHTML = outEdges.length ? outEdges.map(e => `<li onclick="focusNode('${e.data('target')}')"><span>${e.data('target')}</span> <span class="code-text">${e.data('label') || e.data('type')}</span></li>`).join('') : '<li>None</li>';
  document.getElementById('outboundCount').textContent = outEdges.length;

  // Highlight Neighborhood
  highlightNeighborhood(data.id);
}

function highlightNeighborhood(nodeId) {
  cy.elements().removeClass('highlighted blast-target blast-impacted blast-edge path-edge cut-vertex cycle-node dimmed');
  const node = cy.getElementById(nodeId);
  if (!node.length) return;

  const neighborhood = node.neighborhood().add(node);
  cy.elements().difference(neighborhood).addClass('dimmed');
  neighborhood.removeClass('dimmed');
  node.addClass('highlighted');
}

function clearHighlights() {
  cy.elements().removeClass('highlighted blast-target blast-impacted blast-edge path-edge cut-vertex cycle-node dimmed');
}

function focusNode(id) {
  const node = cy.getElementById(id);
  if (node.length) {
    cy.animate({
      center: { eles: node },
      zoom: 1.4
    }, { duration: 400 });
    inspectNode(node.data());
  }
}

// Simulate Blast Radius
async function runBlastRadiusSimulation() {
  if (!selectedNode) {
    alert('Please select a symbol or component on the graph first.');
    return;
  }

  try {
    const res = await fetch(`/api/impact?id=${encodeURIComponent(selectedNode.id)}&symbol=${encodeURIComponent(selectedNode.label)}`);
    const report = await res.json();

    // Update Drawer UI
    const riskBadge = document.getElementById('riskBadge');
    riskBadge.textContent = `${report.risk_score}/100 ${report.risk_level} RISK`;
    riskBadge.className = `risk-badge ${report.risk_level.toLowerCase()}`;

    document.getElementById('riskMeterFill').style.width = `${Math.min(100, report.risk_score)}%`;
    document.getElementById('riskMeterScore').textContent = `${report.risk_score} / 100`;

    // Impacted Tests
    const testsSection = document.getElementById('impactedTestsSection');
    const testsList = document.getElementById('impactedTestsList');
    if (report.impacted_test_files && report.impacted_test_files.length > 0) {
      testsSection.classList.remove('hidden');
      document.getElementById('impactedTestsCount').textContent = report.impacted_test_files.length;
      document.getElementById('testCmdBanner').textContent = report.recommended_test_command || 'go test ./...';
      testsList.innerHTML = report.impacted_test_files.map(t => `<li><svg class="icon-sm" aria-hidden="true" style="margin-right:6px;"><use href="#icon-beaker"/></svg>${t}</li>`).join('');
    } else {
      testsSection.classList.add('hidden');
    }

    // Highlight Blast Radius on Canvas
    cy.elements().addClass('dimmed');
    const targetEle = cy.getElementById(selectedNode.id);
    targetEle.removeClass('dimmed').addClass('blast-target');

    if (report.direct_dependents) {
      report.direct_dependents.forEach(d => {
        const ele = cy.getElementById(d.id);
        if (ele.length) ele.removeClass('dimmed').addClass('blast-impacted');
      });
    }

    if (report.transitive_dependents) {
      report.transitive_dependents.forEach(t => {
        const ele = cy.getElementById(t.id);
        if (ele.length) ele.removeClass('dimmed').addClass('blast-impacted');
      });
    }

    document.getElementById('hudStatus').textContent = `💥 Blast Radius: ${report.total_impacted_nodes || 0} nodes impacted (${report.risk_score}/100 ${report.risk_level} Risk)`;
  } catch (err) {
    console.error('Blast radius simulation failed:', err);
  }
}

// Trace Call Path Between Two Symbols
async function tracePath(src, tgt) {
  try {
    const res = await fetch(`/api/paths?source=${encodeURIComponent(src)}&target=${encodeURIComponent(tgt)}`);
    const data = await res.json();

    if (!data.found || !data.path.length) {
      alert(`No direct or transitive call path found between ${src} and ${tgt}`);
      return;
    }

    cy.elements().addClass('dimmed');
    const pathNodes = cy.collection();

    data.path.forEach(id => {
      const node = cy.getElementById(id);
      if (node.length) {
        node.removeClass('dimmed').addClass('highlighted');
        pathNodes.merge(node);
      }
    });

    for (let i = 0; i < data.path.length - 1; i++) {
      const edge = cy.edges(`[source = "${data.path[i]}"][target = "${data.path[i+1]}"]`);
      edge.removeClass('dimmed').addClass('path-edge');
    }

    cy.animate({
      fit: { eles: pathNodes, padding: 60 }
    }, { duration: 500 });

    document.getElementById('hudStatus').innerHTML = `<svg class="icon-sm" aria-hidden="true" style="vertical-align:-3px; margin-right:4px;"><use href="#icon-lightning"/></svg> Traced Path: ${data.path.length} hops from ${src} to ${tgt}`;
  } catch (e) {
    console.error('Path trace error:', e);
  }
}

// Load Tab 2: Intelligence & Smells Radar
async function loadIntelligenceData() {
  try {
    const res = await fetch('/api/intelligence');
    const data = await res.json();

    // 1. Patterns Grid
    const patternGrid = document.getElementById('patternGrid');
    if (data.patterns && data.patterns.length > 0) {
      patternGrid.innerHTML = data.patterns.map(p => {
        const conf = Math.round((p.confidence || 0.8) * 100);
        return `
        <div class="pattern-card">
          <svg class="icon pattern-icon" aria-hidden="true"><use href="#icon-star"/></svg>
          <div class="pattern-title-row">
            <span class="pattern-title">${p.name || p.title || 'Architectural Pattern'}</span>
          </div>
          <p class="pattern-desc">${p.description || 'Inferred architectural design pattern.'}</p>
          <div class="confidence-bar-wrap">
            <div class="confidence-fill" style="width: ${conf}%"></div>
          </div>
          <span class="confidence-text">${conf}% Match</span>
        </div>
      `}).join('');
    } else {
      patternGrid.innerHTML = `
        <div class="pattern-card">
          <svg class="icon pattern-icon" aria-hidden="true"><use href="#icon-star"/></svg>
          <div class="pattern-title-row">
            <span class="pattern-title">DDD Bounded Context</span>
          </div>
          <p class="pattern-desc">Domain-Driven Design boundaries enforced across internal packages.</p>
          <div class="confidence-bar-wrap">
            <div class="confidence-fill" style="width: 80%"></div>
          </div>
          <span class="confidence-text">80% Match</span>
        </div>
        <div class="pattern-card">
          <svg class="icon pattern-icon" aria-hidden="true"><use href="#icon-star"/></svg>
          <div class="pattern-title-row">
            <span class="pattern-title">Repository Pattern</span>
          </div>
          <p class="pattern-desc">Data access abstraction layer decoupling domain services from storage.</p>
          <div class="confidence-bar-wrap">
            <div class="confidence-fill" style="width: 70%"></div>
          </div>
          <span class="confidence-text">70% Match</span>
        </div>
      `;
    }

    // 2. Smells Table
    currentSmellsData = data.smells || [];
    document.getElementById('intelSmellCount').textContent = currentSmellsData.length;
    renderSmellsTable();

    // 3. Hotspots Leaderboard
    const hotspotsGrid = document.getElementById('hotspotsGrid');
    const hotspots = (data.metrics && data.metrics.top_hotspots) || [];
    if (hotspots.length > 0) {
      const maxCa = Math.max(...hotspots.map(h => h.fan_in || 0), 1);
      hotspotsGrid.innerHTML = hotspots.slice(0, 6).map((h, i) => {
        const rank = (i + 1).toString().padStart(2, '0');
        const relativeCa = Math.min(((h.fan_in || 0) / maxCa) * 100, 100);
        return `
        <div class="hotspot-card">
          <div class="hotspot-rank">${rank}</div>
          <div class="hotspot-body">
            <div class="hotspot-title-row">
              <span class="hotspot-name">${h.name}</span>
              <span class="hotspot-metrics">
                Ca: ${h.fan_in || 0} &nbsp;·&nbsp; Ce: ${h.fan_out || 0} &nbsp;·&nbsp; PR: ${((h.page_rank || 0) * 1000).toFixed(2)}
              </span>
            </div>
            <div class="hotspot-bar-wrap">
              <div class="hotspot-bar-fill" style="width: ${relativeCa}%"></div>
            </div>
          </div>
        </div>
      `}).join('');
    }

    // Note: Tab 5 Metrics Dashboard is now populated via loadMetricsData()
  } catch (err) {
    console.error('Error loading intelligence data:', err);
  }
}

function renderSmellsTable() {
  const tbody = document.getElementById('smellsTableBody');
  if (currentSmellsData.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5">No active architectural smells detected. Excellent code health!</td></tr>';
    return;
  }
  
  tbody.innerHTML = currentSmellsData.map(s => {
    const sev = (s.severity || 'low').toLowerCase();
    return `
    <tr>
      <td><span class="severity-pill ${sev}">${s.severity || 'LOW'}</span></td>
      <td><strong>${s.title || s.name}</strong></td>
      <td><span class="code-text">${s.category || 'STRUCTURAL'}</span></td>
      <td>${s.description || '-'}</td>
      <td><span class="code-text">${(s.nodes || []).slice(0, 3).join(', ') || '-'}</span></td>
    </tr>
  `}).join('');
}

function handleSmellsSort(column, thElement) {
  if (currentSmellsSort.column === column) {
    currentSmellsSort.asc = !currentSmellsSort.asc;
  } else {
    currentSmellsSort.column = column;
    currentSmellsSort.asc = true;
  }
  
  // Update header arrows
  document.querySelectorAll('#smellsTable th .sort-arrow').forEach(el => el.textContent = '↕');
  const arrow = currentSmellsSort.asc ? '↓' : '↑';
  thElement.querySelector('.sort-arrow').textContent = arrow;
  
  // Sort data
  currentSmellsData.sort((a, b) => {
    const severityOrder = { 'critical': 4, 'high': 3, 'medium': 2, 'low': 1 };
    let valA, valB;
    
    if (column === 'severity') {
      valA = severityOrder[(a.severity || 'low').toLowerCase()] || 0;
      valB = severityOrder[(b.severity || 'low').toLowerCase()] || 0;
    } else {
      valA = a[column] || '';
      valB = b[column] || '';
    }
    
    if (valA < valB) return currentSmellsSort.asc ? -1 : 1;
    if (valA > valB) return currentSmellsSort.asc ? 1 : -1;
    return 0;
  });
  
  renderSmellsTable();
}

// Load Tab 3: Timeline Data
async function loadTimelineData() {
  try {
    const res = await fetch('/api/timeline');
    const data = await res.json();
    const timeline = data.timeline || [];
    const stream = document.getElementById('timelineStream');

    if (timeline.length > 0) {
      let currentMonth = '';
      
      const streamHTML = [];
      timeline.slice(0, 50).forEach(item => {
        const date = new Date(item.timestamp);
        const monthStr = date.toLocaleDateString('default', { month: 'long', year: 'numeric' }).toUpperCase();
        
        if (monthStr !== currentMonth) {
          streamHTML.push(`<div class="timeline-month-marker">${monthStr}</div>`);
          currentMonth = monthStr;
        }
        
        const intentClass = (item.intent || 'FEATURE').toLowerCase().replace('_', '-');
        streamHTML.push(`
          <div class="timeline-event-card ${intentClass}" data-intent="${item.intent || 'FEATURE'}">
            <div class="timeline-dot ${intentClass}"></div>
            <div class="event-body">
              <div class="event-top-row">
                <span class="event-title">${item.title}</span>
                <span class="event-meta">${item.commit_hash ? item.commit_hash.substring(0, 7) : ''} · ${date.toLocaleDateString()}</span>
              </div>
              <p class="event-desc">${item.description || item.title}</p>
            </div>
          </div>
        `);
      });
      stream.innerHTML = streamHTML.join('');
    } else {
      stream.innerHTML = '<p style="color: var(--text-muted); padding-left: 48px;">No recorded evolution timeline events yet.</p>';
    }

    // Filter Chips for Timeline
    document.querySelectorAll('.timeline-filter-bar .filter-chip').forEach(chip => {
      chip.addEventListener('click', () => {
        document.querySelectorAll('.timeline-filter-bar .filter-chip').forEach(c => c.classList.remove('active'));
        chip.classList.add('active');
        const filter = chip.getAttribute('data-filter');

        document.querySelectorAll('.timeline-event-card').forEach(card => {
          if (filter === 'ALL' || card.getAttribute('data-intent') === filter) {
            card.style.display = 'flex';
          } else {
            card.style.display = 'none';
          }
        });
      });
    });
  } catch (err) {
    console.error('Error loading timeline data:', err);
  }
}

// Initialize Mermaid.js
function initMermaid() {
  if (window.mermaid) {
    mermaid.initialize({
      startOnLoad: false,
      theme: 'dark',
      themeVariables: {
        darkMode: true,
        background: '#080b11',
        primaryColor: '#8b5cf6',
        primaryBorderColor: '#8b5cf6',
        lineColor: '#06b6d4',
        textColor: '#f8fafc'
      }
    });
  }
}

// Load Tab 4: Marbles Catalog
async function loadMarblesCatalog() {
  try {
    const res = await fetch('/api/marbles');
    const items = await res.json();
    const list = document.getElementById('marblesList');

    if (items.length > 0) {
      list.innerHTML = items.map((item, idx) => {
        let typeClass = 'type-default';
        const t = (item.type || '').toUpperCase();
        if (t.includes('C4')) typeClass = 'type-c4';
        else if (t.includes('UML')) typeClass = 'type-uml';
        else if (t.includes('CALL')) typeClass = 'type-callgraph';
        
        return `
        <li class="marble-nav-item ${idx === 0 ? 'active' : ''}" onclick="selectMarble('${item.name}', '${item.title}', '${item.type}')">
          <div class="marble-thumb ${typeClass}"></div>
          <div class="marble-nav-info">
            <span class="marble-type-tag">${item.type}</span>
            <span class="marble-title">${item.title}</span>
          </div>
        </li>
      `}).join('');

      selectMarble(items[0].name, items[0].title, items[0].type);
    } else {
      list.innerHTML = '<li>No marbles diagrams found.</li>';
    }
  } catch (err) {
    console.error('Error loading marbles catalog:', err);
  }
}

async function selectMarble(name, title, type) {
  document.querySelectorAll('.marble-nav-item').forEach(item => {
    if (item.innerText.includes(title)) item.classList.add('active');
    else item.classList.remove('active');
  });

  document.getElementById('currentDiagTitle').textContent = title;
  document.getElementById('currentDiagType').textContent = type;

  try {
    const res = await fetch(`/api/marbles?name=${encodeURIComponent(name)}`);
    const data = await res.json();

    const stage = document.getElementById('mermaidStage');
    const rawPre = document.getElementById('mermaidSource');
    rawPre.textContent = data.mermaid;

    if (window.mermaid && data.mermaid) {
      stage.innerHTML = `<div class="mermaid">${data.mermaid}</div>`;
      try {
        await mermaid.run({ nodes: stage.querySelectorAll('.mermaid') });
      } catch (err) {
        console.error('Mermaid render error:', err);
      }
    }
  } catch (err) {
    console.error('Error fetching marble diagram:', err);
  }
}

// Event Listeners Setup
function initEventHandlers() {
  // Granularity Selector
  document.getElementById('granularitySelect').addEventListener('change', e => {
    currentGranularity = e.target.value;
    loadGraphData();
  });

  // Layout Switcher
  document.getElementById('layoutSelect').addEventListener('change', e => {
    applyLayout(e.target.value);
  });

  // Fit Viewport
  document.getElementById('btnFit').addEventListener('click', () => {
    cy.animate({ fit: { padding: 40 } }, { duration: 400 });
  });

  // Coupling Slider
  document.getElementById('minDegreeRange').addEventListener('input', e => {
    document.getElementById('minDegreeVal').textContent = e.target.value;
  });
  document.getElementById('minDegreeRange').addEventListener('change', e => {
    renderFilteredGraph();
  });

  // Export PNG — updated bg colors
  document.getElementById('btnExportPNG').addEventListener('click', () => {
    const isLight = document.body.classList.contains('light-theme');
    const png64 = cy.png({ full: true, scale: 2, bg: isLight ? '#eef0f7' : '#050810' });
    const link = document.createElement('a');
    link.download = `glassmarble-architecture-${currentGranularity}.png`;
    link.href = png64;
    link.click();
    showToast('Architecture graph exported as PNG', 'success');
  });

  // Dark/Light Theme Toggle — swap moon/sun icons
  document.getElementById('btnThemeToggle').addEventListener('click', () => {
    document.body.classList.toggle('light-theme');
    const isLight = document.body.classList.contains('light-theme');
    const moonIcon = document.querySelector('#btnThemeToggle .icon-moon');
    const sunIcon  = document.querySelector('#btnThemeToggle .icon-sun');
    if (moonIcon) moonIcon.style.display = isLight ? 'none' : '';
    if (sunIcon)  sunIcon.style.display  = isLight ? '' : 'none';
    initCytoscape();
    renderFilteredGraph();
  });

  // Toggle Mermaid Source
  document.getElementById('btnToggleMermaidSource').addEventListener('click', () => {
    const pre = document.getElementById('mermaidSource');
    const stage = document.getElementById('mermaidStage');
    pre.classList.toggle('hidden');
    stage.classList.toggle('hidden');
  });

  // Drawer Controls
  document.getElementById('btnCloseDrawer').addEventListener('click', () => {
    document.getElementById('inspector').classList.add('hidden');
    clearHighlights();
  });

  document.getElementById('btnSimulateBlast').addEventListener('click', runBlastRadiusSimulation);
  document.getElementById('btnBlastRadius').addEventListener('click', () => {
    if (selectedNode) runBlastRadiusSimulation();
    else showToast('Please click a symbol or component node on the graph first.', 'warning');
  });

  // Copy Test Command — use toast instead of alert
  document.getElementById('btnCopyTestCmd').addEventListener('click', () => {
    const cmd = document.getElementById('testCmdBanner').textContent;
    navigator.clipboard.writeText(cmd).then(() => {
      showToast('Test command copied to clipboard!', 'success');
    }).catch(() => {
      showToast('Failed to copy to clipboard', 'error');
    });
  });

  // Cut Vertices (Single Points of Failure)
  document.getElementById('btnCutVertices').addEventListener('click', async () => {
    cutVerticesMode = !cutVerticesMode;
    const btn = document.getElementById('btnCutVertices');
    btn.classList.toggle('active', cutVerticesMode);

    if (cutVerticesMode) {
      const res = await fetch('/api/algorithms/cutvertices');
      const data = await res.json();
      cy.elements().addClass('dimmed');

      const aps = data.articulation_points || [];
      aps.forEach(ap => {
        const ele = cy.getElementById(ap.id);
        if (ele.length) ele.removeClass('dimmed').addClass('cut-vertex');
      });
      document.getElementById('hudStatus').innerHTML = `<svg class="icon-sm" aria-hidden="true" style="vertical-align:-3px; margin-right:4px; color:var(--accent-rose);"><use href="#icon-warning-circle"/></svg> Found ${aps.length} Single Points of Failure (Cut Vertices)`;
    } else {
      clearHighlights();
      document.getElementById('hudStatus').textContent = 'Ready';
    }
  });

  // Cycles Detector
  document.getElementById('btnCycles').addEventListener('click', async () => {
    cyclesMode = !cyclesMode;
    const btn = document.getElementById('btnCycles');
    btn.classList.toggle('active', cyclesMode);

    if (cyclesMode) {
      const res = await fetch('/api/algorithms/cycles');
      const data = await res.json();
      cy.elements().addClass('dimmed');

      const cycles = data.cycles || [];
      cycles.forEach(cyc => {
        cyc.forEach(nid => {
          const ele = cy.getElementById(nid);
          if (ele.length) ele.removeClass('dimmed').addClass('cycle-node');
        });
      });
      document.getElementById('hudStatus').textContent = `🔄 Found ${cycles.length} Circular Dependency Loops`;
    } else {
      clearHighlights();
      document.getElementById('hudStatus').textContent = 'Ready';
    }
  });

  // PageRank Heatmap
  document.getElementById('btnPageRank').addEventListener('click', async () => {
    pageRankMode = !pageRankMode;
    const btn = document.getElementById('btnPageRank');
    btn.classList.toggle('active', pageRankMode);

    if (pageRankMode) {
      const res = await fetch('/api/algorithms/pagerank');
      const data = await res.json();
      const ranks = data.ranks || [];

      cy.elements().addClass('dimmed');
      ranks.forEach(r => {
        const ele = cy.getElementById(r.id);
        if (ele.length) {
          ele.removeClass('dimmed').addClass('highlighted');
        }
      });
      document.getElementById('hudStatus').innerHTML = `<svg class="icon-sm" aria-hidden="true" style="vertical-align:-3px; margin-right:4px; color:var(--accent-amber);"><use href="#icon-star"/></svg> Highlighting Top ${ranks.length} Central PageRank Hotspots`;
    } else {
      clearHighlights();
      document.getElementById('hudStatus').textContent = 'Ready';
    }
  });

  // Trace Path UI
  const pathBar = document.getElementById('pathTraceBar');
  document.getElementById('btnPathTrace').addEventListener('click', () => {
    pathBar.classList.toggle('hidden');
  });
  document.getElementById('btnCloseTrace').addEventListener('click', () => {
    pathBar.classList.add('hidden');
    clearHighlights();
  });
  document.getElementById('btnRunTrace').addEventListener('click', () => {
    const src = document.getElementById('pathSourceInput').value.trim();
    const tgt = document.getElementById('pathTargetInput').value.trim();
    if (src && tgt) tracePath(src, tgt);
  });

  // Smells Radar
  document.getElementById('btnSmellsRadar').addEventListener('click', async () => {
    smellsMode = !smellsMode;
    const btn = document.getElementById('btnSmellsRadar');
    btn.classList.toggle('active', smellsMode);

    if (smellsMode) {
      const res = await fetch('/api/smells');
      const smells = await res.json();
      cy.elements().addClass('dimmed');

      smells.forEach(sm => {
        if (sm.nodes) {
          sm.nodes.forEach(nid => {
            const ele = cy.getElementById(nid);
            if (ele.length) ele.removeClass('dimmed').addClass('blast-target');
          });
        }
      });
      document.getElementById('hudStatus').textContent = `⚠️ Smells Radar: Highlighted ${smells.length} architectural issues`;
    } else {
      clearHighlights();
      document.getElementById('hudStatus').textContent = 'Ready';
    }
  });

  // Filter Listeners
  document.querySelectorAll('#layerFilters input, #kindFilters input').forEach(input => {
    input.addEventListener('change', renderFilteredGraph);
  });

  const degreeSlider = document.getElementById('minDegreeRange');
  degreeSlider.addEventListener('input', e => {
    document.getElementById('minDegreeVal').textContent = e.target.value;
    renderFilteredGraph();
  });

  // Omni-Search
  const searchInput = document.getElementById('omniSearch');
  const searchDropdown = document.getElementById('searchDropdown');

  window.addEventListener('keydown', e => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      searchInput.focus();
    }
  });

  searchInput.addEventListener('input', async e => {
    const q = e.target.value.trim();
    if (!q) {
      searchDropdown.classList.add('hidden');
      return;
    }

    try {
      const res = await fetch(`/api/search?q=${encodeURIComponent(q)}`);
      const matches = await res.json();
      searchDropdown.innerHTML = '';

      if (matches.length > 0) {
        searchDropdown.classList.remove('hidden');
        matches.forEach(m => {
          const item = document.createElement('div');
          item.className = 'search-result-item';
          item.innerHTML = `
            <div>
              <div class="item-name">${m.name}</div>
              <div class="item-file">${m.file || ''}:${m.line || 1}</div>
            </div>
            <span class="kind-pill ${m.kind ? m.kind.toLowerCase() : 'struct'}">${m.kind || 'SYMBOL'}</span>
          `;
          item.addEventListener('click', () => {
            focusNode(m.id);
            searchDropdown.classList.add('hidden');
            searchInput.value = m.name;
          });
          searchDropdown.appendChild(item);
        });
      } else {
        searchDropdown.classList.add('hidden');
      }
    } catch (err) {
      console.error('Search error:', err);
    }
  });
}

// ============================================================
// SKELETON LOADER UI
// ============================================================
function setSkeleton(id, show) {
  const el = document.getElementById(id);
  if (!el) return;
  if (show) {
    el.classList.add('skeleton');
  } else {
    el.classList.remove('skeleton');
  }
    return;
  }
  
  tbody.innerHTML = currentMetricsData.map(c => {
    const iVal = c.instability || 0;
    let statusClass = 'emerald';
    let statusText = 'Stable';
    if (iVal > 0.7) { statusClass = 'rose'; statusText = 'Volatile'; }
    else if (iVal > 0.3) { statusClass = 'amber'; statusText = 'Flexible'; }
    
    return `
    <tr style="cursor:pointer;" onclick="jumpToGraphNode('${c.id}')">
      <td><strong>${c.name}</strong></td>
      <td><span class="badge-tag">${c.kind || 'COMPONENT'}</span></td>
      <td><span class="code-text">${(c.directories || []).length} dirs</span></td>
      <td>
        <div style="display:flex; align-items:center; gap:8px;">
          <span style="width:20px;">${c.ca || 0}</span>
          <div class="m-card-bar" style="width:40px; margin:0;"><div class="m-card-bar-fill" style="width:${Math.min((c.ca||0)*5, 100)}%; background:var(--accent-cyan);"></div></div>
        </div>
      </td>
      <td>
        <div style="display:flex; align-items:center; gap:8px;">
          <span style="width:20px;">${c.ce || 0}</span>
          <div class="m-card-bar" style="width:40px; margin:0;"><div class="m-card-bar-fill" style="width:${Math.min((c.ce||0)*5, 100)}%; background:var(--accent-purple);"></div></div>
        </div>
      </td>
      <td>
        <div style="display:flex; align-items:center; gap:8px;">
          <span style="width:30px;">${iVal.toFixed(2)}</span>
          <div class="m-card-bar" style="width:40px; margin:0;"><div class="m-card-bar-fill" style="width:${iVal * 100}%; background:var(--accent-amber);"></div></div>
        </div>
      </td>
      <td><span class="severity-pill ${statusClass}">${statusText}</span></td>
    </tr>
  `}).join('');
}

function handleMetricsSort(column, thElement) {
  if (currentMetricsSort.column === column) {
    currentMetricsSort.asc = !currentMetricsSort.asc;
  } else {
    currentMetricsSort.column = column;
    currentMetricsSort.asc = true;
  }
  
  document.querySelectorAll('#componentsTable th .sort-arrow').forEach(el => el.textContent = '↕');
  const arrow = currentMetricsSort.asc ? '↓' : '↑';
  thElement.querySelector('.sort-arrow').textContent = arrow;
  
  currentMetricsData.sort((a, b) => {
    let valA = a[column] || 0;
    let valB = b[column] || 0;
    if (column === 'name' || column === 'kind') {
      valA = a[column] || '';
      valB = b[column] || '';
    } else if (column === 'dirs') {
      valA = (a.directories || []).length;
      valB = (b.directories || []).length;
    }
    
    if (valA < valB) return currentMetricsSort.asc ? -1 : 1;
    if (valA > valB) return currentMetricsSort.asc ? 1 : -1;
    return 0;
  });
  
  renderMetricsTable();
}

function jumpToGraphNode(nodeId) {
  const tabBtn = document.querySelector('.nav-tab[data-tab="tab-graph"]');
  if (tabBtn) tabBtn.click();
  
  setTimeout(() => {
    if (!cy) return;
    const node = cy.getElementById(nodeId);
    if (node.length) {
      inspectNode(node.data());
      cy.animate({ fit: { eles: node, padding: 100 } }, { duration: 500 });
      node.flashClass('highlighted', 2000);
    } else {
      showToast('Node not visible in current graph view.', 'warning');
    }
  }, 100);
}

// ============================================================
// TOAST NOTIFICATIONS
// ============================================================
function showToast(message, type = 'info') {
  let container = document.getElementById('toastContainer');
  if (!container) {
    container = document.createElement('div');
    container.id = 'toastContainer';
    document.body.appendChild(container);
  }

  const toast = document.createElement('div');
  toast.className = `toast toast-${type}`;
  
  let icon = 'icon-info';
  if (type === 'error') icon = 'icon-x-circle';
  if (type === 'warning') icon = 'icon-warning';
  
  toast.innerHTML = `<svg class="icon-sm" aria-hidden="true"><use href="#${icon}"/></svg><span>${message}</span>`;
  container.appendChild(toast);

  requestAnimationFrame(() => {
    toast.classList.add('show');
  });

  setTimeout(() => {
    toast.classList.remove('show');
    toast.style.transform = 'translateX(20px)';
    setTimeout(() => toast.remove(), 300);
  }, 3000);
}
