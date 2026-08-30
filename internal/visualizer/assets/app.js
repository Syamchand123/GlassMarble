// GlassMarble Architecture Knowledge Graph Visualizer
// High-performance Cytoscape.js application controller

let cy = null;
let rawElements = [];
let selectedNode = null;
let blastMode = false;
let smellsMode = false;
let currentLayout = "cose";

// Theme Configuration
const THEME = {
  dark: {
    bg: "#090d16",
    nodeStruct: "#06b6d4",
    nodeIface: "#10b981",
    nodeFunc: "#8b5cf6",
    nodeDb: "#d946ef",
    nodeEntry: "#f59e0b",
    nodeDefault: "#64748b",
    nodeText: "#f8fafc",
    compoundBg: "rgba(30, 41, 59, 0.4)",
    compoundBorder: "#334155",
    edgeDefault: "rgba(100, 116, 139, 0.35)",
    edgeHighlight: "#8b5cf6",
    edgeBlast: "#ef4444",
    edgeCycle: "#f43f5e"
  },
  light: {
    bg: "#f8fafc",
    nodeStruct: "#0891b2",
    nodeIface: "#059669",
    nodeFunc: "#7c3aed",
    nodeDb: "#c026d3",
    nodeEntry: "#d97706",
    nodeDefault: "#94a3b8",
    nodeText: "#0f172a",
    compoundBg: "rgba(241, 245, 249, 0.6)",
    compoundBorder: "#cbd5e1",
    edgeDefault: "rgba(148, 163, 184, 0.5)",
    edgeHighlight: "#7c3aed",
    edgeBlast: "#dc2626",
    edgeCycle: "#e11d48"
  }
};

window.addEventListener("DOMContentLoaded", () => {
  initCytoscape();
  initEventHandlers();
  loadGraphData();
});

// Initialize Cytoscape Instance
function initCytoscape() {
  const t = document.body.classList.contains("light-theme") ? THEME.light : THEME.dark;

  cy = cytoscape({
    container: document.getElementById("cy"),
    boxSelectionEnabled: false,
    autounselectify: false,
    wheelSensitivity: 0.25,
    minZoom: 0.15,
    maxZoom: 3.5,
    style: [
      // Normal Nodes
      {
        selector: 'node[!is_compound]',
        style: {
          'label': 'data(label)',
          'font-family': '-apple-system, BlinkMacSystemFont, Segoe UI, Roboto, sans-serif',
          'font-size': '11px',
          'font-weight': '600',
          'color': t.nodeText,
          'text-valign': 'bottom',
          'text-margin-y': '6px',
          'text-background-opacity': 0.85,
          'text-background-color': t.bg,
          'text-background-padding': '3px',
          'text-background-shape': 'roundrectangle',
          'width': 'mapData(in_degree, 0, 15, 26, 52)',
          'height': 'mapData(in_degree, 0, 15, 26, 52)',
          'background-color': ele => getNodeColor(ele.data('kind')),
          'border-width': 2,
          'border-color': '#ffffff',
          'border-opacity': 0.4,
          'transition-property': 'background-color, border-color, border-width, opacity',
          'transition-duration': '0.2s'
        }
      },
      // Kind-Specific Shapes
      { selector: 'node[kind = "STRUCT"], node[kind = "CLASS"]', style: { 'shape': 'roundrectangle' } },
      { selector: 'node[kind = "INTERFACE"]', style: { 'shape': 'diamond' } },
      { selector: 'node[kind = "FUNCTION"], node[kind = "METHOD"]', style: { 'shape': 'ellipse' } },
      { selector: 'node[kind = "DATABASE"]', style: { 'shape': 'barrel' } },
      { selector: 'node[kind = "ENTRYPOINT"]', style: { 'shape': 'star', 'border-color': '#f59e0b', 'border-width': 3 } },

      // Compound Parent Package Containers
      {
        selector: 'node[?is_compound]',
        style: {
          'label': 'data(label)',
          'font-family': 'ui-monospace, monospace',
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

      // Edges
      {
        selector: 'edge',
        style: {
          'width': 1.5,
          'line-color': t.edgeDefault,
          'target-arrow-color': t.edgeDefault,
          'target-arrow-shape': 'triangle',
          'curve-style': 'bezier',
          'arrow-scale': 0.9,
          'opacity': 0.7,
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
          'width': 2.5
        }
      },

      // Interactive States
      {
        selector: 'node:selected',
        style: {
          'border-color': '#ffffff',
          'border-width': 4,
          'border-opacity': 1.0,
          'shadow-blur': 20,
          'shadow-color': '#8b5cf6',
          'shadow-opacity': 0.8
        }
      },
      {
        selector: 'node.highlighted',
        style: {
          'border-color': '#06b6d4',
          'border-width': 3,
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
          'shadow-opacity': 0.9
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
        selector: 'edge.blast-edge',
        style: {
          'line-color': '#ef4444',
          'target-arrow-color': '#ef4444',
          'width': 3,
          'opacity': 1.0
        }
      },
      {
        selector: 'edge.path-edge',
        style: {
          'line-color': '#06b6d4',
          'target-arrow-color': '#06b6d4',
          'width': 3.5,
          'opacity': 1.0
        }
      },
      {
        selector: '.dimmed',
        style: {
          'opacity': 0.15
        }
      }
    ]
  });

  // Node Selection Handler
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
    case 'DATABASE': return '#d946ef';
    case 'ENTRYPOINT': return '#f59e0b';
    default: return '#64748b';
  }
}

// Fetch Graph Data from Backend
async function loadGraphData() {
  try {
    const [graphRes, statusRes] = await Promise.all([
      fetch('/api/graph'),
      fetch('/api/status')
    ]);

    rawElements = await graphRes.json();
    const statusData = await statusRes.json();

    // Populate Status Stats
    document.getElementById('statTotalNodes').textContent = statusData.nodes_count || rawElements.filter(e => e.group === 'nodes' && !e.data.is_compound).length;
    document.getElementById('statTotalEdges').textContent = statusData.edges_count || rawElements.filter(e => e.group === 'edges').length;
    document.getElementById('statTotalFiles').textContent = statusData.files_count || '-';
    document.getElementById('statTotalSmells').textContent = statusData.smells_count || '0';

    renderFilteredGraph();
    document.getElementById('hudStatus').textContent = `Loaded ${statusData.nodes_count} symbols & ${statusData.edges_count} relationships`;
  } catch (err) {
    console.error('Failed to load architecture graph:', err);
    document.getElementById('hudStatus').textContent = 'Error loading graph from backend';
  }
}

// Apply Filters & Re-run Layout
function renderFilteredGraph() {
  if (!cy || !rawElements.length) return;

  const activeLayers = new Set(
    Array.from(document.querySelectorAll('#layerFilters input:checked')).map(cb => cb.value)
  );
  const activeKinds = new Set(
    Array.from(document.querySelectorAll('#kindFilters input:checked')).map(cb => cb.value)
  );
  const minInDegree = parseInt(document.getElementById('minDegreeRange').value, 10) || 0;

  const filtered = rawElements.filter(el => {
    if (el.group === 'nodes') {
      if (el.data.is_compound) return true;
      if (el.data.layer && !activeLayers.has(el.data.layer)) return false;
      if (el.data.kind && !activeKinds.has(el.data.kind)) return false;
      if (el.data.in_degree < minInDegree) return false;
      return true;
    }
    return true; // Edges will be filtered if their endpoints are missing
  });

  cy.elements().remove();
  cy.add(filtered);
  applyLayout(currentLayout);
}

// Apply Cytoscape Layout
function applyLayout(name) {
  currentLayout = name;
  let layoutOptions = { name: 'cose', animate: true, animationDuration: 600 };

  switch (name) {
    case 'breadthfirst':
      layoutOptions = {
        name: 'breadthfirst',
        directed: true,
        spacingFactor: 1.4,
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
        idealEdgeLength: 60,
        nodeOverlap: 20,
        refresh: 20,
        fit: true,
        padding: 30,
        randomize: false,
        componentSpacing: 100,
        nodeRepulsion: 400000,
        edgeElasticity: 100,
        nestingFactor: 5,
        gravity: 80,
        numIter: 1000,
        initialTemp: 200,
        coolingFactor: 0.95,
        minTemp: 1.0,
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

  document.getElementById('nodeFilePath').textContent = data.file ? `${data.file}:${data.line || 1}` : '-';
  document.getElementById('nodePackage').textContent = data.parent || 'root';
  document.getElementById('nodeLayer').textContent = (data.layer || 'common').toUpperCase();

  document.getElementById('mInDegree').textContent = data.in_degree || 0;
  document.getElementById('mOutDegree').textContent = data.out_degree || 0;
  document.getElementById('mPageRank').textContent = (data.pagerank || 0).toFixed(3);

  // Inbound & Outbound Lists
  const inEdges = cy.edges(`[target = "${data.id}"]`);
  const outEdges = cy.edges(`[source = "${data.id}"]`);

  const inboundList = document.getElementById('inboundList');
  inboundList.innerHTML = inEdges.length ? inEdges.map(e => `<li onclick="focusNode('${e.data('source')}')"><span>${e.data('source')}</span> <span class="code-text">${e.data('type')}</span></li>`).join('') : '<li>None</li>';
  document.getElementById('inboundCount').textContent = inEdges.length;

  const outboundList = document.getElementById('outboundList');
  outboundList.innerHTML = outEdges.length ? outEdges.map(e => `<li onclick="focusNode('${e.data('target')}')"><span>${e.data('target')}</span> <span class="code-text">${e.data('type')}</span></li>`).join('') : '<li>None</li>';
  document.getElementById('outboundCount').textContent = outEdges.length;

  // Highlight Neighborhood
  highlightNeighborhood(data.id);
}

function highlightNeighborhood(nodeId) {
  cy.elements().removeClass('highlighted blast-target blast-impacted blast-edge path-edge dimmed');
  const node = cy.getElementById(nodeId);
  if (!node.length) return;

  const neighborhood = node.neighborhood().add(node);
  cy.elements().difference(neighborhood).addClass('dimmed');
  neighborhood.removeClass('dimmed');
  node.addClass('highlighted');
}

function clearHighlights() {
  cy.elements().removeClass('highlighted blast-target blast-impacted blast-edge path-edge dimmed');
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
    alert('Please select a symbol on the graph to simulate blast radius.');
    return;
  }

  try {
    const res = await fetch(`/api/impact?symbol=${encodeURIComponent(selectedNode.label)}`);
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
      testsList.innerHTML = report.impacted_test_files.map(t => `<li>🧪 ${t}</li>`).join('');
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

    document.getElementById('hudStatus').textContent = `💥 Blast Radius: ${report.total_impacted_nodes || 0} symbols impacted across ${report.total_impacted_files || 0} files`;
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

    document.getElementById('hudStatus').textContent = `⚡ Traced Path: ${data.path.length} hops from ${src} to ${tgt}`;
  } catch (e) {
    console.error('Path trace error:', e);
  }
}

// Event Listeners Setup
function initEventHandlers() {
  // Layout Switcher
  document.getElementById('layoutSelect').addEventListener('change', e => {
    applyLayout(e.target.value);
  });

  // Fit Viewport
  document.getElementById('btnFit').addEventListener('click', () => {
    cy.animate({ fit: { padding: 40 } }, { duration: 400 });
  });

  // Export PNG
  document.getElementById('btnExportPNG').addEventListener('click', () => {
    const png64 = cy.png({ full: true, scale: 2, bg: document.body.classList.contains('light-theme') ? '#f8fafc' : '#090d16' });
    const link = document.createElement('a');
    link.download = 'glassmarble-architecture.png';
    link.href = png64;
    link.click();
  });

  // Dark/Light Theme Toggle
  document.getElementById('btnThemeToggle').addEventListener('click', () => {
    document.body.classList.toggle('light-theme');
    initCytoscape();
    renderFilteredGraph();
  });

  // Drawer Controls
  document.getElementById('btnCloseDrawer').addEventListener('click', () => {
    document.getElementById('inspector').classList.add('hidden');
    clearHighlights();
  });

  document.getElementById('btnSimulateBlast').addEventListener('click', runBlastRadiusSimulation);
  document.getElementById('btnBlastRadius').addEventListener('click', () => {
    if (selectedNode) runBlastRadiusSimulation();
    else alert('Please click a symbol node on the graph first.');
  });

  // Copy Test Command
  document.getElementById('btnCopyTestCmd').addEventListener('click', () => {
    const cmd = document.getElementById('testCmdBanner').textContent;
    navigator.clipboard.writeText(cmd);
    alert('Copied test command to clipboard!');
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
