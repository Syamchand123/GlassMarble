package render

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderHTMLStudio creates a self-contained, interactive HTML diagram studio.
func RenderHTMLStudio(diagramMarkup string, t types.DiagramType, summary *types.GraphSummary, themeName string) string {
	if themeName == "" {
		themeName = "modern"
	}

	escapedMarkup := strings.ReplaceAll(diagramMarkup, "\\", "\\\\")
	escapedMarkup = strings.ReplaceAll(escapedMarkup, "`", "\\`")

	var statsHTML string
	if summary != nil {
		statsHTML = fmt.Sprintf(
			`<div class="stat-badge">Nodes: <strong>%d</strong></div>
			 <div class="stat-badge">Edges: <strong>%d</strong></div>
			 <div class="stat-badge">Density: <strong>%.4f</strong></div>
			 <div class="stat-badge">Components: <strong>%d</strong></div>`,
			summary.NodeCount, summary.EdgeCount, summary.Density, summary.ConnectedComponents,
		)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>GlassMarble Studio · %s</title>
    <script src="https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"></script>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            background: #0f172a;
            color: #f8fafc;
            height: 100vh;
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }
        header {
            background: #1e293b;
            border-bottom: 1px solid #334155;
            padding: 10px 20px;
            display: flex;
            align-items: center;
            justify-content: space-between;
            z-index: 10;
        }
        .title-group {
            display: flex;
            align-items: center;
            gap: 12px;
        }
        .logo {
            font-size: 16px;
            font-weight: 800;
            letter-spacing: -0.5px;
            background: linear-gradient(135deg, #38bdf8, #818cf8);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }
        .diag-pill {
            background: #334155;
            color: #94a3b8;
            font-size: 11px;
            font-weight: 600;
            padding: 2px 8px;
            border-radius: 9999px;
            text-transform: uppercase;
        }
        .controls {
            display: flex;
            align-items: center;
            gap: 8px;
        }
        .btn {
            background: #334155;
            color: #f8fafc;
            border: 1px solid #475569;
            padding: 6px 12px;
            font-size: 12px;
            font-weight: 500;
            border-radius: 6px;
            cursor: pointer;
            transition: all 0.15s ease;
        }
        .btn:hover {
            background: #475569;
            border-color: #64748b;
        }
        .btn-primary {
            background: #4f46e5;
            border-color: #6366f1;
        }
        .btn-primary:hover {
            background: #4338ca;
        }
        .search-input {
            background: #0f172a;
            border: 1px solid #334155;
            color: #f8fafc;
            padding: 6px 10px;
            font-size: 12px;
            border-radius: 6px;
            width: 180px;
            outline: none;
        }
        .search-input:focus {
            border-color: #818cf8;
        }
        .hud {
            position: absolute;
            bottom: 16px;
            left: 16px;
            display: flex;
            gap: 8px;
            z-index: 10;
        }
        .stat-badge {
            background: rgba(30, 41, 59, 0.85);
            backdrop-filter: blur(8px);
            border: 1px solid #334155;
            padding: 4px 10px;
            border-radius: 6px;
            font-size: 11px;
            color: #94a3b8;
        }
        .stat-badge strong {
            color: #f8fafc;
        }
        #canvas-container {
            flex: 1;
            position: relative;
            cursor: grab;
            overflow: hidden;
            background: #ffffff;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        #canvas-container:active {
            cursor: grabbing;
        }
        #diagram-target {
            transform-origin: center center;
            transition: transform 0.05s ease-out;
        }
        .highlighted {
            filter: drop-shadow(0 0 8px #38bdf8) !important;
        }
        .dimmed {
            opacity: 0.25 !important;
            transition: opacity 0.2s ease;
        }
    </style>
</head>
<body>
    <header>
        <div class="title-group">
            <div class="logo">GlassMarble</div>
            <div class="diag-pill">%s</div>
        </div>
        <div class="controls">
            <input type="text" id="search-box" class="search-input" placeholder="Search node / symbol..." />
            <button class="btn" onclick="resetTransform()">Reset View</button>
            <button class="btn" onclick="zoom(1.2)">Zoom +</button>
            <button class="btn" onclick="zoom(0.8)">Zoom −</button>
            <button class="btn btn-primary" onclick="exportSVG()">Export SVG</button>
        </div>
    </header>

    <div id="canvas-container">
        <div id="diagram-target" class="mermaid">
%s
        </div>
    </div>

    <div class="hud">
        %s
    </div>

    <script>
        mermaid.initialize({
            startOnLoad: true,
            theme: '%s' === 'dark' ? 'dark' : 'default',
            securityLevel: 'loose',
            flowchart: { useMaxWidth: false, htmlLabels: true, curve: 'basis' }
        });

        let scale = 1;
        let panX = 0;
        let panY = 0;
        let isDragging = false;
        let startX, startY;

        const container = document.getElementById('canvas-container');
        const target = document.getElementById('diagram-target');
        const searchBox = document.getElementById('search-box');

        function updateTransform() {
            target.style.transform = `+"`"+`translate(${panX}px, ${panY}px) scale(${scale})`+"`"+`;
        }

        function resetTransform() {
            scale = 1;
            panX = 0;
            panY = 0;
            updateTransform();
        }

        function zoom(factor) {
            scale *= factor;
            scale = Math.min(Math.max(0.1, scale), 5);
            updateTransform();
        }

        container.addEventListener('wheel', (e) => {
            e.preventDefault();
            const delta = e.deltaY > 0 ? 0.9 : 1.1;
            zoom(delta);
        }, { passive: false });

        container.addEventListener('mousedown', (e) => {
            if (e.target.closest('input, button')) return;
            isDragging = true;
            startX = e.clientX - panX;
            startY = e.clientY - panY;
        });

        window.addEventListener('mousemove', (e) => {
            if (!isDragging) return;
            panX = e.clientX - startX;
            panY = e.clientY - startY;
            updateTransform();
        });

        window.addEventListener('mouseup', () => { isDragging = false; });

        searchBox.addEventListener('input', (e) => {
            const query = e.target.value.toLowerCase().trim();
            const nodes = target.querySelectorAll('.node, .cluster');
            if (!query) {
                nodes.forEach(n => {
                    n.classList.remove('highlighted');
                    n.classList.remove('dimmed');
                });
                return;
            }
            nodes.forEach(n => {
                const text = n.textContent.toLowerCase();
                if (text.includes(query)) {
                    n.classList.add('highlighted');
                    n.classList.remove('dimmed');
                } else {
                    n.classList.remove('highlighted');
                    n.classList.add('dimmed');
                }
            });
        });

        function exportSVG() {
            const svg = target.querySelector('svg');
            if (!svg) return;
            const serializer = new XMLSerializer();
            const source = serializer.serializeToString(svg);
            const blob = new Blob([source], { type: 'image/svg+xml;charset=utf-8' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = '%s.svg';
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
            URL.revokeObjectURL(url);
        }
    </script>
</body>
</html>`,
		string(t), string(t), diagramMarkup, statsHTML, themeName, string(t),
	)
}
