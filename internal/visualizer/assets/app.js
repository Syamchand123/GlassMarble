// GlassMarble Live Architecture Visualizer
// Zero-dependency interactive force-directed graph renderer

let graphData = { nodes: [], edges: [] };
let selectedNode = null;
let blastRadiusMode = false;
let blastRadiusSet = new Set();
let zoom = 1.0;
let pan = { x: 0, y: 0 };
let isDragging = false;
let dragStart = { x: 0, y: 0 };
let draggedNode = null;

const canvas = document.getElementById("graphCanvas");
const ctx = canvas.getContext("2d");

// Color Palette
const COLORS = {
  STRUCT: "#06b6d4",
  CLASS: "#06b6d4",
  INTERFACE: "#10b981",
  FUNCTION: "#8b5cf6",
  METHOD: "#a78bfa",
  DATABASE: "#d946ef",
  NETWORK_IO: "#f59e0b",
  MODULE: "#3b82f6",
  DEFAULT: "#64748b",
  BLAST_TARGET: "#ef4444",
  BLAST_IMPACT: "#f97316",
  EDGE_DEFAULT: "rgba(100, 116, 139, 0.4)",
  EDGE_HIGHLIGHT: "rgba(239, 68, 68, 0.9)",
};

window.addEventListener("DOMContentLoaded", () => {
  resizeCanvas();
  window.addEventListener("resize", resizeCanvas);
  initEventListeners();
  loadData();
  requestAnimationFrame(simulationLoop);
});

function resizeCanvas() {
  const container = canvas.parentElement;
  canvas.width = container.clientWidth * window.devicePixelRatio;
  canvas.height = container.clientHeight * window.devicePixelRatio;
  ctx.scale(window.devicePixelRatio, window.devicePixelRatio);
}

async function loadData() {
  try {
    const [graphRes, statusRes] = await Promise.all([
      fetch("/api/graph"),
      fetch("/api/status")
    ]);

    graphData = await graphRes.json();
    const statusData = await statusRes.json();

    // Update stats
    document.getElementById("statNodes").textContent = statusData.nodes_count || graphData.nodes.length;
    document.getElementById("statEdges").textContent = statusData.edges_count || graphData.edges.length;
    document.getElementById("statFiles").textContent = statusData.indexed_files || "-";
    document.getElementById("statSmells").textContent = statusData.smells_count || "0";

    initGraphLayout();
    document.getElementById("hudStatus").textContent = `Loaded ${graphData.nodes.length} nodes & ${graphData.edges.length} edges`;
  } catch (err) {
    console.error("Failed to load graph data:", err);
    document.getElementById("hudStatus").textContent = "Error loading graph data";
  }
}

function initGraphLayout() {
  const width = canvas.width / (2 * window.devicePixelRatio);
  const height = canvas.height / (2 * window.devicePixelRatio);

  pan.x = width;
  pan.y = height;

  graphData.nodes.forEach((n, i) => {
    const angle = (i / graphData.nodes.length) * 2 * Math.PI;
    const radius = 100 + Math.random() * 250;
    n.x = Math.cos(angle) * radius;
    n.y = Math.sin(angle) * radius;
    n.vx = 0;
    n.vy = 0;
    n.radius = n.kind === "STRUCT" || n.kind === "CLASS" ? 14 : 10;
  });
}

function simulationLoop() {
  updateSimulation();
  renderGraph();
  requestAnimationFrame(simulationLoop);
}

function updateSimulation() {
  // Simple force-directed physics
  const repulsion = 1200;
  const damping = 0.88;

  for (let i = 0; i < graphData.nodes.length; i++) {
    const n1 = graphData.nodes[i];
    for (let j = i + 1; j < graphData.nodes.length; j++) {
      const n2 = graphData.nodes[j];
      const dx = n2.x - n1.x;
      const dy = n2.y - n1.y;
      const dist = Math.sqrt(dx * dx + dy * dy) || 1;

      if (dist < 400) {
        const force = repulsion / (dist * dist);
        const fx = (dx / dist) * force;
        const fy = (dy / dist) * force;
        n1.vx -= fx;
        n1.vy -= fy;
        n2.vx += fx;
        n2.vy += fy;
      }
    }
  }

  // Edge springs
  graphData.edges.forEach(e => {
    const src = graphData.nodes.find(n => n.id === e.source_id);
    const tgt = graphData.nodes.find(n => n.id === e.target_id);
    if (!src || !tgt) return;

    const dx = tgt.x - src.x;
    const dy = tgt.y - src.y;
    const dist = Math.sqrt(dx * dx + dy * dy) || 1;
    const force = (dist - 80) * 0.02;

    const fx = (dx / dist) * force;
    const fy = (dy / dist) * force;
    src.vx += fx;
    src.vy += fy;
    tgt.vx -= fx;
    tgt.vy -= fy;
  });

  // Center gravity
  graphData.nodes.forEach(n => {
    if (n === draggedNode) return;
    n.vx -= n.x * 0.005;
    n.vy -= n.y * 0.005;

    n.vx *= damping;
    n.vy *= damping;

    n.x += n.vx;
    n.y += n.vy;
  });
}

function renderGraph() {
  const w = canvas.width / window.devicePixelRatio;
  const h = canvas.height / window.devicePixelRatio;

  ctx.clearRect(0, 0, w, h);
  ctx.save();
  ctx.translate(pan.x, pan.y);
  ctx.scale(zoom, zoom);

  // Render Edges
  graphData.edges.forEach(e => {
    const src = graphData.nodes.find(n => n.id === e.source_id);
    const tgt = graphData.nodes.find(n => n.id === e.target_id);
    if (!src || !tgt) return;

    const isHighlighted = selectedNode && (selectedNode.id === src.id || selectedNode.id === tgt.id);
    const isBlast = blastRadiusMode && (blastRadiusSet.has(src.id) && blastRadiusSet.has(tgt.id));

    ctx.beginPath();
    ctx.moveTo(src.x, src.y);
    ctx.lineTo(tgt.x, tgt.y);

    if (isBlast || isHighlighted) {
      ctx.strokeStyle = COLORS.EDGE_HIGHLIGHT;
      ctx.lineWidth = 2.5;
    } else {
      ctx.strokeStyle = COLORS.EDGE_DEFAULT;
      ctx.lineWidth = 1;
    }
    ctx.stroke();
  });

  // Render Nodes
  graphData.nodes.forEach(n => {
    const isSelected = selectedNode && selectedNode.id === n.id;
    const isBlastTarget = isSelected && blastRadiusMode;
    const isBlastImpacted = blastRadiusMode && blastRadiusSet.has(n.id) && !isSelected;

    let color = COLORS[n.kind] || COLORS.DEFAULT;
    if (isBlastTarget) color = COLORS.BLAST_TARGET;
    else if (isBlastImpacted) color = COLORS.BLAST_IMPACT;

    ctx.beginPath();
    ctx.arc(n.x, n.y, n.radius, 0, 2 * Math.PI);
    ctx.fillStyle = color;
    ctx.fill();

    if (isSelected || isBlastImpacted) {
      ctx.strokeStyle = "#ffffff";
      ctx.lineWidth = 3;
      ctx.stroke();
    }

    // Node Name Label
    ctx.fillStyle = "#f8fafc";
    ctx.font = "11px -apple-system, sans-serif";
    ctx.textAlign = "center";
    ctx.fillText(n.name, n.x, n.y + n.radius + 14);
  });

  ctx.restore();
}

function initEventListeners() {
  // Pan and Zoom
  canvas.addEventListener("wheel", e => {
    e.preventDefault();
    const factor = e.deltaY < 0 ? 1.1 : 0.9;
    zoom = Math.max(0.2, Math.min(4.0, zoom * factor));
  });

  canvas.addEventListener("mousedown", e => {
    const pos = getMousePos(e);
    const clickedNode = findNodeAtPos(pos.x, pos.y);

    if (clickedNode) {
      draggedNode = clickedNode;
      selectNode(clickedNode);
    } else {
      isDragging = true;
      dragStart = { x: e.clientX - pan.x, y: e.clientY - pan.y };
    }
  });

  window.addEventListener("mousemove", e => {
    if (draggedNode) {
      const pos = getMousePos(e);
      draggedNode.x = pos.x;
      draggedNode.y = pos.y;
    } else if (isDragging) {
      pan.x = e.clientX - dragStart.x;
      pan.y = e.clientY - dragStart.y;
    }
  });

  window.addEventListener("mouseup", () => {
    isDragging = false;
    draggedNode = null;
  });

  // UI Buttons
  document.getElementById("resetViewBtn").addEventListener("click", () => {
    zoom = 1.0;
    pan.x = canvas.width / (2 * window.devicePixelRatio);
    pan.y = canvas.height / (2 * window.devicePixelRatio);
  });

  document.getElementById("themeToggleBtn").addEventListener("click", () => {
    document.body.classList.toggle("light-theme");
  });

  document.getElementById("closeDrawerBtn").addEventListener("click", () => {
    document.getElementById("inspectorDrawer").classList.add("hidden");
    selectedNode = null;
  });

  document.getElementById("blastRadiusBtn").addEventListener("click", toggleBlastRadius);
  document.getElementById("simulateImpactBtn").addEventListener("click", toggleBlastRadius);

  // Search
  const searchInput = document.getElementById("searchInput");
  const searchResults = document.getElementById("searchResults");

  searchInput.addEventListener("input", e => {
    const q = e.target.value.trim().toLowerCase();
    if (!q) {
      searchResults.classList.add("hidden");
      return;
    }

    const matches = graphData.nodes.filter(n =>
      n.name.toLowerCase().includes(q) || (n.file_spec && n.file_spec.path && n.file_spec.path.toLowerCase().includes(q))
    ).slice(0, 8);

    searchResults.innerHTML = "";
    if (matches.length > 0) {
      searchResults.classList.remove("hidden");
      matches.forEach(m => {
        const item = document.createElement("div");
        item.className = "search-item";
        item.innerHTML = `<strong>${m.name}</strong> <span style="color:#94a3b8;font-size:11px;">${m.kind}</span>`;
        item.addEventListener("click", () => {
          selectNode(m);
          panToNode(m);
          searchResults.classList.add("hidden");
        });
        searchResults.appendChild(item);
      });
    } else {
      searchResults.classList.add("hidden");
    }
  });
}

function getMousePos(e) {
  const rect = canvas.getBoundingClientRect();
  const rawX = e.clientX - rect.left;
  const rawY = e.clientY - rect.top;
  return {
    x: (rawX - pan.x) / zoom,
    y: (rawY - pan.y) / zoom,
  };
}

function findNodeAtPos(x, y) {
  for (let i = graphData.nodes.length - 1; i >= 0; i--) {
    const n = graphData.nodes[i];
    const dx = n.x - x;
    const dy = n.y - y;
    if (Math.sqrt(dx * dx + dy * dy) <= n.radius + 5) {
      return n;
    }
  }
  return null;
}

async function selectNode(node) {
  selectedNode = node;
  document.getElementById("hudSelection").textContent = `Selected: ${node.name} (${node.kind})`;

  const drawer = document.getElementById("inspectorDrawer");
  drawer.classList.remove("hidden");

  document.getElementById("nodeDetailName").textContent = node.name;
  document.getElementById("nodeDetailKind").textContent = node.kind;
  document.getElementById("nodeDetailFile").textContent = node.file_spec ? node.file_spec.path : "-";
  document.getElementById("nodeDetailPackage").textContent = node.file_spec ? node.file_spec.path.split("/").slice(0, -1).join("/") : "-";

  // Query callers & deps
  const inEdges = graphData.edges.filter(e => e.target_id === node.id);
  const outEdges = graphData.edges.filter(e => e.source_id === node.id);

  const callersList = document.getElementById("callersList");
  callersList.innerHTML = inEdges.length ? inEdges.map(e => `<li>${e.source_id} (${e.type})</li>`).join("") : "<li>None</li>";
  document.getElementById("callerCount").textContent = inEdges.length;

  const depsList = document.getElementById("depsList");
  depsList.innerHTML = outEdges.length ? outEdges.map(e => `<li>${e.target_id} (${e.type})</li>`).join("") : "<li>None</li>";
  document.getElementById("depCount").textContent = outEdges.length;

  document.getElementById("nodeDetailImpact").textContent = `${inEdges.length} direct callers`;
}

function panToNode(node) {
  const w = canvas.width / (2 * window.devicePixelRatio);
  const h = canvas.height / (2 * window.devicePixelRatio);
  pan.x = w - node.x * zoom;
  pan.y = h - node.y * zoom;
}

async function toggleBlastRadius() {
  if (!selectedNode) {
    alert("Please click a node on the canvas to simulate its blast radius.");
    return;
  }

  blastRadiusMode = !blastRadiusMode;
  const btn = document.getElementById("blastRadiusBtn");
  btn.style.backgroundColor = blastRadiusMode ? "#ef4444" : "";

  if (blastRadiusMode) {
    try {
      const res = await fetch(`/api/impact?symbol=${encodeURIComponent(selectedNode.name)}`);
      const impact = await res.json();
      blastRadiusSet = new Set(impact.impacted_files ? impact.impacted_files : []);
      if (impact.direct_dependents) {
        impact.direct_dependents.forEach(d => blastRadiusSet.add(d.id));
      }
      if (impact.transitive_dependents) {
        impact.transitive_dependents.forEach(t => blastRadiusSet.add(t.id));
      }
      document.getElementById("hudStatus").textContent = `Blast Radius: ${impact.total_impacted_nodes || 0} nodes impacted (Risk: ${impact.risk_level || 'LOW'})`;
    } catch (e) {
      console.error(e);
    }
  } else {
    blastRadiusSet.clear();
    document.getElementById("hudStatus").textContent = "Blast Radius Spotlight Disabled";
  }
}
