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

// Theme Configuration
const THEME = {
  dark: {
    bg: "#080b11",
    nodeStruct: "#06b6d4",
    nodeIface: "#10b981",
    nodeFunc: "#8b5cf6",
    nodeComp: "#6366f1",
    nodeDb: "#d946ef",
    nodeEntry: "#f59e0b",
    nodeDefault: "#64748b",
    nodeText: "#f8fafc",
    compoundBg: "rgba(21, 28, 46, 0.4)",
    compoundBorder: "#334155",
    edgeDefault: "rgba(100, 116, 139, 0.45)",
    edgeHighlight: "#8b5cf6",
    edgeBlast: "#ef4444",
    edgeCycle: "#f43f5e"
  },
  light: {
    bg: "#f8fafc",
    nodeStruct: "#0891b2",
    nodeIface: "#059669",
    nodeFunc: "#7c3aed",
    nodeComp: "#4f46e5",
    nodeDb: "#c026d3",
    nodeEntry: "#d97706",
    nodeDefault: "#94a3b8",
    nodeText: "#0f172a",
    compoundBg: "rgba(241, 245, 249, 0.6)",
    compoundBorder: "#cbd5e1",
    edgeDefault: "rgba(148, 163, 184, 0.6)",
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
  loadGraphData();
  loadIntelligenceData();
  loadTimelineData();
  loadMarblesCatalog();
});

// Initialize Tab Navigation
function initTabs() {
  document.querySelectorAll('.nav-tab').forEach(btn => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.nav-tab').forEach(b => b.classList.remove('active'));
      document.querySelectorAll('.tab-pane').forEach(p => p.classList.remove('active'));

      btn.classList.add('active');
      const targetTab = btn.getAttribute('data-tab');
      document.getElementById(targetTab).classList.add('active');

      const graphControls = document.getElementById('graphViewControls');
      if (targetTab === 'tab-graph') {
        graphControls.style.display = 'flex';
        if (cy) cy.resize();
      } else {
        graphControls.style.display = 'none';
      }
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

    // Populate Status Stats
    document.getElementById('statTotalNodes').textContent = statusData.nodes_count || '-';
    document.getElementById('statTotalEdges').textContent = statusData.edges_count || '-';
    document.getElementById('statTotalFiles').textContent = statusData.files_count || '-';
    document.getElementById('statTotalSmells').textContent = statusData.smells_count || '0';

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

    document.getElementById('hudStatus').textContent = `⚡ Traced Path: ${data.path.length} hops from ${src} to ${tgt}`;
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
      patternGrid.innerHTML = data.patterns.map(p => `
        <div class="pattern-card">
          <div class="pattern-title-row">
            <span class="pattern-title">${p.name || p.title || 'Architectural Pattern'}</span>
            <span class="pattern-confidence">${Math.round((p.confidence || 0.8) * 100)}% Match</span>
          </div>
          <p class="pattern-desc">${p.description || 'Inferred architectural design pattern.'}</p>
        </div>
      `).join('');
    } else {
      patternGrid.innerHTML = `
        <div class="pattern-card">
          <div class="pattern-title-row">
            <span class="pattern-title">DDD Bounded Context</span>
            <span class="pattern-confidence">80% Match</span>
          </div>
          <p class="pattern-desc">Domain-Driven Design boundaries enforced across internal packages.</p>
        </div>
        <div class="pattern-card">
          <div class="pattern-title-row">
            <span class="pattern-title">Repository Pattern</span>
            <span class="pattern-confidence">70% Match</span>
          </div>
          <p class="pattern-desc">Data access abstraction layer decoupling domain services from storage.</p>
        </div>
      `;
    }

    // 2. Smells Table
    const smellsTableBody = document.getElementById('smellsTableBody');
    const smellsList = data.smells || [];
    document.getElementById('intelSmellCount').textContent = smellsList.length;

    if (smellsList.length > 0) {
      smellsTableBody.innerHTML = smellsList.map(s => `
        <tr>
          <td><span class="severity-pill ${(s.severity || 'low').toLowerCase()}">${s.severity || 'LOW'}</span></td>
          <td><strong>${s.title || s.name}</strong></td>
          <td><span class="code-text">${s.category || 'STRUCTURAL'}</span></td>
          <td>${s.description || '-'}</td>
          <td><span class="code-text">${(s.nodes || []).slice(0, 3).join(', ') || '-'}</span></td>
        </tr>
      `).join('');
    } else {
      smellsTableBody.innerHTML = '<tr><td colspan="5">No active architectural smells detected. Excellent code health!</td></tr>';
    }

    // 3. Hotspots Leaderboard
    const hotspotsGrid = document.getElementById('hotspotsGrid');
    const hotspots = (data.metrics && data.metrics.top_hotspots) || [];
    if (hotspots.length > 0) {
      hotspotsGrid.innerHTML = hotspots.slice(0, 6).map(h => `
        <div class="hotspot-card">
          <div class="hotspot-name">${h.name}</div>
          <div class="hotspot-metrics">
            <span>Fan-In: <strong>${h.fan_in || 0}</strong></span>
            <span>Fan-Out: <strong>${h.fan_out || 0}</strong></span>
            <span>PageRank: <strong>${((h.page_rank || 0) * 1000).toFixed(2)}</strong></span>
          </div>
        </div>
      `).join('');
    }

    // 4. Populate Tab 5 Metrics Dashboard
    if (data.metrics) {
      document.getElementById('metricInstability').textContent = (data.metrics.instability || 0.5).toFixed(2);
      document.getElementById('metricDensity').textContent = (data.metrics.graph_density || 0.0001).toFixed(4);
      document.getElementById('metricDeadCode').textContent = data.metrics.dead_code_node_count ? data.metrics.dead_code_node_count.toLocaleString() : '0';
      document.getElementById('metricLayerViolations').textContent = data.metrics.layer_violation_count || '0';
    }

    // 5. Components Breakdown Table (Tab 5)
    const compTableBody = document.getElementById('componentsTableBody');
    if (data.components && data.components.length > 0) {
      compTableBody.innerHTML = data.components.map(c => {
        const inst = c.instability || 0;
        let statusBadge = '<span class="severity-pill low">STABLE CORE</span>';
        if (inst > 0.7) statusBadge = '<span class="severity-pill high">VOLATILE</span>';
        else if (inst > 0.3) statusBadge = '<span class="severity-pill medium">BALANCED</span>';

        return `
          <tr>
            <td><strong>${c.name}</strong></td>
            <td><span class="kind-pill comp">${c.kind || 'MODULE'}</span></td>
            <td><span class="code-text">${(c.directories || []).join(', ') || '-'}</span></td>
            <td>${c.ca || 0}</td>
            <td>${c.ce || 0}</td>
            <td>${inst.toFixed(2)}</td>
            <td>${statusBadge}</td>
          </tr>
        `;
      }).join('');
    }
  } catch (err) {
    console.error('Error loading intelligence data:', err);
  }
}

// Load Tab 3: Timeline Data
async function loadTimelineData() {
  try {
    const res = await fetch('/api/timeline');
    const data = await res.json();
    const timeline = data.timeline || [];
    const stream = document.getElementById('timelineStream');

    if (timeline.length > 0) {
      stream.innerHTML = timeline.slice(0, 50).map(item => {
        const intentClass = (item.intent || 'FEATURE').toLowerCase().replace('_', '');
        return `
          <div class="timeline-event-card ${intentClass}" data-intent="${item.intent || 'FEATURE'}">
            <div class="event-top-row">
              <span class="event-title">${item.title}</span>
              <span class="event-meta">${item.commit_hash ? item.commit_hash.substring(0, 7) : ''} · ${new Date(item.timestamp).toLocaleDateString()}</span>
            </div>
            <p class="event-desc">${item.description || item.title}</p>
          </div>
        `;
      }).join('');
    } else {
      stream.innerHTML = '<p style="color: var(--text-muted);">No recorded evolution timeline events yet.</p>';
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
      list.innerHTML = items.map((item, idx) => `
        <li class="marble-nav-item ${idx === 0 ? 'active' : ''}" onclick="selectMarble('${item.name}', '${item.title}', '${item.type}')">
          <span class="marble-type-tag">${item.type}</span>
          <span class="marble-title">${item.title}</span>
        </li>
      `).join('');

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

  // Export PNG
  document.getElementById('btnExportPNG').addEventListener('click', () => {
    const isLight = document.body.classList.contains('light-theme');
    const png64 = cy.png({ full: true, scale: 2, bg: isLight ? '#f8fafc' : '#080b11' });
    const link = document.createElement('a');
    link.download = `glassmarble-architecture-${currentGranularity}.png`;
    link.href = png64;
    link.click();
  });

  // Dark/Light Theme Toggle
  document.getElementById('btnThemeToggle').addEventListener('click', () => {
    document.body.classList.toggle('light-theme');
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
    else alert('Please click a symbol or component node on the graph first.');
  });

  // Copy Test Command
  document.getElementById('btnCopyTestCmd').addEventListener('click', () => {
    const cmd = document.getElementById('testCmdBanner').textContent;
    navigator.clipboard.writeText(cmd);
    alert('Copied test command to clipboard!');
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
      document.getElementById('hudStatus').textContent = `🔴 Found ${aps.length} Single Points of Failure (Cut Vertices)`;
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
      document.getElementById('hudStatus').textContent = `⭐ Highlighting Top ${ranks.length} Central PageRank Hotspots`;
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
