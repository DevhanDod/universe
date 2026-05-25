**Always run Composer 2 sub-agents for the actual tasks.** Do not write code, edit files, or run commands yourself -- launch Task sub-agents with `model: "fast"` to do the work. This spawns free Composer 2 sub-agents. Never omit the `model` parameter — without it, sub-agents inherit the parent's expensive model (e.g. claude-4.6-opus-high).

# Universe — Interactive Graph Visualizer

## Overview

Build a web-based interactive graph visualization for Universe's knowledge graph output. The visualizer loads the `graph.json` file produced by `universe analyze` and renders it as a force-directed graph — similar to GitNexus's visualization — where every code entity (function, struct, class, package, import) is a node and every relationship (calls, imports, depends_on, contains) is an edge.

The visualizer is a **standalone HTML file** with embedded JS/CSS. No build step, no framework, no npm. Just one HTML file that opens in a browser and loads the graph JSON.

```bash
# After analyzing a project
universe analyze /path/to/project

# Open the visualizer — it reads .universe/graph.json
open visualizer.html
# OR serve it
cd /path/to/project && python3 -m http.server 8080
# then open http://localhost:8080/visualizer.html
```

---

## Data Format (CRITICAL — match exactly)

The `graph.json` file produced by Universe has this exact structure:

```json
{
  "nodes": {
    "main:main.go:main": {
      "id": "main:main.go:main",
      "name": "main",
      "type": "package",
      "file_path": "cmd/universe/main.go",
      "package": "main",
      "start_line": 24,
      "end_line": 32,
      "signature": "",
      "metadata": {
        "exported": "true",
        "is_test": "false"
      }
    },
    "main:main.go:fmt": {
      "id": "main:main.go:fmt",
      "name": "fmt",
      "type": "import",
      "file_path": "cmd/universe/main.go",
      "package": "main",
      "start_line": 5,
      "end_line": 5,
      "metadata": {
        "path": "fmt"
      }
    }
  },
  "edges": [
    {
      "from": "main:main.go:main",
      "to": "main:main.go:fmt",
      "type": "imports",
      "metadata": {}
    },
    {
      "from": "main:main.go:runAnalyze",
      "to": "main:main.go:buildAnalyzer",
      "type": "calls",
      "metadata": {
        "callee_expression": "buildAnalyzer"
      }
    }
  ]
}
```

### Key facts about the data:

1. **`nodes` is an OBJECT (map), not an array.** Keys are node IDs. Each value is a Node.
2. **`edges` is an ARRAY.** Each element has `from`, `to`, `type`, and optional `metadata`.
3. **Node ID format:** `"package:filename:symbolname"` (for Go) or `"module.path:filename:symbolname"` (for Python)
4. **Node types:** `"package"`, `"file"`, `"function"`, `"method"`, `"struct"`, `"interface"`, `"type"`, `"variable"`, `"import"`, `"class"`, `"module"`
5. **Edge types:** `"imports"`, `"calls"`, `"implements"`, `"depends_on"`, `"contains"`, `"returns"`, `"receives"`, `"inherits"`
6. **Metadata fields vary** — common ones: `"exported"`, `"is_test"`, `"path"` (for imports), `"receiver"` (for methods), `"callee_expression"` (for calls), `"decorators"` (for Python)

---

## Tech Stack

| Component | Technology | Why |
|-----------|-----------|-----|
| Rendering | **Sigma.js v2** (WebGL) | Handles 5000+ nodes smoothly, GPU-accelerated |
| Graph layout | **graphology** + **graphology-layout-forceatlas2** | Force-directed layout algorithm, works with Sigma |
| Graph library | **graphology** | In-memory graph data structure, powers Sigma |
| Search | Built-in filter on graphology | Fast node search/filter |
| UI | Vanilla HTML/CSS/JS | No build step needed |

**CDN imports (include in the HTML):**
```html
<script src="https://cdnjs.cloudflare.com/ajax/libs/sigma.js/2.4.0/sigma.min.js"></script>
```

IMPORTANT: Since CDN availability for sigma v2 + graphology can be tricky, use an alternative approach:

**Use `d3-force` for layout + Canvas/WebGL for rendering.** This is more reliable:
```html
<script src="https://cdnjs.cloudflare.com/ajax/libs/d3/7.8.5/d3.min.js"></script>
```

D3 v7 from cdnjs is stable and reliable. Use `d3.forceSimulation` for the physics layout and render on an HTML5 Canvas for performance with large graphs.

---

## Architecture

### Single HTML File Structure

```
visualizer.html
├── <style> ... </style>           (all CSS inline)
├── <div id="app">
│   ├── <div id="toolbar">         (top bar: search, stats, controls)
│   ├── <div id="sidebar">         (left: file explorer tree)
│   ├── <canvas id="graph">        (center: force-directed graph)
│   └── <div id="detail-panel">    (right: node detail on click)
│   └── <div id="filters">         (bottom/overlay: filter controls)
├── <script> ... </script>          (all JS inline)
```

---

## Features to Build

### 1. Graph Loading

- On page load, show a file picker OR a text input for the path to `graph.json`
- Also support: drag-and-drop the JSON file onto the page
- Also support: URL parameter `?file=path/to/graph.json` for auto-loading
- After loading, convert the Universe graph format into the internal rendering format:
  - `nodes` object → array of `{ id, name, type, package, filePath, x, y, size, color }`
  - `edges` array → array of `{ source, target, type, color }`

### 2. Force-Directed Layout

Use `d3.forceSimulation()` with these forces:

```javascript
const simulation = d3.forceSimulation(nodes)
  .force("link", d3.forceLink(edges).id(d => d.id).distance(80).strength(0.3))
  .force("charge", d3.forceManyBody().strength(-120).distanceMax(500))
  .force("center", d3.forceCenter(width / 2, height / 2))
  .force("collision", d3.forceCollide().radius(d => d.size + 2))
  .force("x", d3.forceX(width / 2).strength(0.05))
  .force("y", d3.forceY(height / 2).strength(0.05));
```

- Nodes with more connections should be larger (scale by degree)
- Cluster nodes by package/module (nodes in the same package attract each other more)
- Add a custom force that groups nodes by `package` field — this creates the visual clusters you see in the GitNexus screenshot

### 3. Color Scheme

Each node type gets a distinct color:

```javascript
const nodeColors = {
  package:   "#6366F1",  // indigo
  file:      "#8B5CF6",  // violet
  function:  "#3B82F6",  // blue
  method:    "#06B6D4",  // cyan
  struct:    "#10B981",  // emerald
  interface: "#F59E0B",  // amber
  type:      "#EC4899",  // pink
  variable:  "#EF4444",  // red
  import:    "#64748B",  // slate (dimmer — imports are noise)
  class:     "#14B8A6",  // teal
  module:    "#A855F7",  // purple
};
```

Edge colors by type (semi-transparent):

```javascript
const edgeColors = {
  imports:     "rgba(100, 116, 139, 0.15)",  // very faint — imports are everywhere
  calls:       "rgba(59, 130, 246, 0.3)",    // blue, more visible — important
  implements:  "rgba(245, 158, 11, 0.4)",    // amber — structural
  depends_on:  "rgba(239, 68, 68, 0.4)",     // red — cross-package
  contains:    "rgba(99, 102, 241, 0.1)",    // very faint — structural noise
  returns:     "rgba(16, 185, 129, 0.2)",
  receives:    "rgba(20, 184, 166, 0.2)",
  inherits:    "rgba(168, 85, 247, 0.4)",    // purple — class hierarchy
};
```

### 4. Canvas Rendering (NOT SVG — for performance)

Render on an HTML5 Canvas for performance. SVG chokes above ~500 nodes.

```javascript
function render() {
  ctx.clearRect(0, 0, width, height);

  // Draw edges first (behind nodes)
  edges.forEach(edge => {
    ctx.beginPath();
    ctx.moveTo(edge.source.x, edge.source.y);
    ctx.lineTo(edge.target.x, edge.target.y);
    ctx.strokeStyle = edgeColors[edge.type] || "rgba(255,255,255,0.05)";
    ctx.lineWidth = edge.highlighted ? 2 : 0.5;
    ctx.stroke();
  });

  // Draw nodes on top
  nodes.forEach(node => {
    ctx.beginPath();
    ctx.arc(node.x, node.y, node.size, 0, 2 * Math.PI);
    ctx.fillStyle = node.highlighted ? brighten(node.color) : node.color;
    ctx.fill();

    // Label for larger nodes only (to avoid clutter)
    if (node.size > 4 || node.highlighted) {
      ctx.fillStyle = "#ffffff";
      ctx.font = `${Math.max(8, node.size * 1.5)}px sans-serif`;
      ctx.fillText(node.name, node.x + node.size + 3, node.y + 3);
    }
  });
}
```

### 5. Interaction

**Zoom & Pan:**
- Mouse wheel to zoom (scale the canvas transform)
- Click + drag on empty space to pan
- Use `d3.zoom()` for this — it handles transform math

**Node Hover:**
- On mouse move, find the nearest node within hover radius
- Highlight the node + all its direct neighbors (connected by edges)
- Dim all other nodes to 20% opacity
- Show a tooltip with: `name`, `type`, `package`, `file_path`

**Node Click:**
- Open the detail panel (right side) with full node info:
  - Name, Type, Package, File Path, Start/End Line
  - Signature (if present)
  - All metadata
  - List of incoming edges (what depends on this)
  - List of outgoing edges (what this depends on)
- Highlight the node and ALL connected nodes (walk edges in both directions)
- Make connected edges brighter/thicker

**Double-Click Node:**
- Center and zoom to that node + its immediate neighborhood

### 6. Top Toolbar

```
[🔍 Search nodes...          ] [Nodes: 423] [Edges: 401] [📊 Stats] [🔄 Re-layout] [📥 Load JSON]
```

- **Search:** type to filter — highlights matching nodes in the graph, dims non-matching. Search matches against `name`, `package`, `id`, and `file_path`.
- **Stats:** shows a popup with graph statistics (same as `universe stats` output)
- **Re-layout:** restarts the force simulation (useful if the layout is messy)
- **Load JSON:** file picker to load a different graph.json

### 7. Left Sidebar — File Explorer

Build a collapsible tree view from the `file_path` fields of all nodes:

```
▾ cmd/
  ▾ universe/
    📄 main.go (45 nodes)
▾ internal/
  ▾ parser/
    📄 go_parser.go (32 nodes)
    📄 python_parser.go (67 nodes)
    📄 registry.go (8 nodes)
  ▾ graph/
    📄 graph.go (18 nodes)
  ▾ models/
    📄 models.go (12 nodes)
```

- Click a file → highlight all nodes from that file in the graph
- Click a directory → highlight all nodes from files in that directory
- Show node count per file
- Collapsible sections for each directory

### 8. Bottom Filter Bar

Toggleable filter chips for node types and edge types:

```
Node types: [✓ function] [✓ method] [✓ struct] [✓ interface] [✓ class] [□ import] [□ variable] [✓ package]
Edge types: [✓ calls] [✓ depends_on] [✓ implements] [□ imports] [□ contains] [✓ inherits]
```

- **Imports and contains are OFF by default** — they add noise. The user can toggle them on.
- When a type is toggled off, those nodes/edges are hidden from the graph and the simulation re-runs without them.
- "Show all" and "Show minimal" presets

### 9. Right Detail Panel

Appears when a node is clicked. Shows:

```
╔════════════════════════════════╗
║ HandleLogin                    ║
║ ┌──────────┐                   ║
║ │ function │  package: main    ║
║ └──────────┘                   ║
║                                ║
║ File: cmd/universe/main.go     ║
║ Lines: 84 — 133                ║
║                                ║
║ Signature:                     ║
║ ┌────────────────────────────┐ ║
║ │ func runAnalyze(cmd        │ ║
║ │   *cobra.Command,          │ ║
║ │   args []string) error     │ ║
║ └────────────────────────────┘ ║
║                                ║
║ Metadata:                      ║
║   exported: true               ║
║   is_test: false               ║
║                                ║
║ ── Incoming (3) ──────────     ║
║ ↑ main.Execute() [calls]       ║
║ ↑ analyzeCmd [contains]        ║
║                                ║
║ ── Outgoing (5) ──────────     ║
║ → buildAnalyzer() [calls]      ║
║ → fmt.Fprintf() [calls]        ║
║ → graph.ExportJSON() [calls]   ║
╚════════════════════════════════╝
```

- Close button (×) to dismiss
- Clicking an incoming/outgoing node in the list should navigate to and highlight that node

### 10. Performance Optimizations

- **Hide labels at low zoom** — only show labels when zoomed in enough
- **LOD (Level of Detail)** — at low zoom, render nodes as simple circles. At high zoom, add labels and borders.
- **Edge bundling** — at low zoom, thin out edges (only show edges for hovered/selected nodes)
- **Viewport culling** — only render nodes/edges visible in the current viewport
- **Quadtree** for hit detection — use `d3.quadtree` for fast nearest-node lookup on hover
- **requestAnimationFrame** — only re-render when the simulation ticks or the user interacts

### 11. Keyboard Shortcuts

- `F` or `/` → Focus search box
- `Escape` → Clear selection, close detail panel
- `+` / `-` → Zoom in / out
- `0` → Reset zoom to fit all nodes
- `H` → Toggle sidebar
- `L` → Re-run layout

---

## UI Design

### Color Theme (dark mode, matching Universe CLI)

```css
:root {
  --bg-primary:    #08080d;
  --bg-secondary:  #0d0d16;
  --bg-panel:      #111118;
  --border:        #1a1a24;
  --text-primary:  #e0e0e0;
  --text-secondary:#888888;
  --text-dim:      #555555;
  --accent:        #6366F1;
  --accent-hover:  #818CF8;
}
```

### Layout

```
┌─────────────────────────────────────────────────────────┐
│  [🔍 Search...]  [Stats: 423 nodes / 401 edges]  [⚙️]  │  ← Top toolbar (50px)
├────────────┬──────────────────────────┬─────────────────┤
│            │                          │                 │
│  File      │     Canvas               │   Detail        │
│  Explorer  │     (force graph)        │   Panel         │
│            │                          │   (on click)    │
│  200px     │     flex                 │   320px         │
│            │                          │                 │
├────────────┴──────────────────────────┴─────────────────┤
│  [✓ function] [✓ struct] [□ import] ...  Filter chips   │  ← Bottom bar (44px)
└─────────────────────────────────────────────────────────┘
```

---

## File: `visualizer.html`

Create a single self-contained HTML file. All CSS in a `<style>` block, all JS in a `<script>` block. The only external dependency is D3.js from CDN:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Universe — Knowledge Graph Visualizer</title>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/d3/7.8.5/d3.min.js"></script>
  <style>
    /* ALL CSS HERE */
  </style>
</head>
<body>
  <!-- ALL HTML HERE -->
  <script>
    /* ALL JS HERE */
  </script>
</body>
</html>
```

---

## Loading the Graph

Support three ways to load:

1. **File input button** — `<input type="file" accept=".json">`
2. **Drag and drop** — drop a JSON file anywhere on the page
3. **Fetch from relative path** — on load, try `fetch('.universe/graph.json')`. If it exists, auto-load. If not, show the file picker.

After loading:

```javascript
function loadGraph(json) {
  // json.nodes is an OBJECT (map), convert to array
  const nodesArray = Object.values(json.nodes).filter(n => n && n.id);

  // json.edges is already an array
  const edgesArray = (json.edges || []).filter(e => e && e.from && e.to);

  // Calculate node degree (number of connections)
  const degree = {};
  edgesArray.forEach(e => {
    degree[e.from] = (degree[e.from] || 0) + 1;
    degree[e.to] = (degree[e.to] || 0) + 1;
  });

  // Assign size based on degree (more connections = bigger)
  const maxDegree = Math.max(...Object.values(degree), 1);
  nodesArray.forEach(n => {
    n.degree = degree[n.id] || 0;
    n.size = 2 + (n.degree / maxDegree) * 12;  // range: 2px to 14px
    n.color = nodeColors[n.type] || "#666";
  });

  // Convert edges: from/to strings → source/target objects (for d3-force)
  const nodeMap = {};
  nodesArray.forEach(n => nodeMap[n.id] = n);

  const validEdges = edgesArray.filter(e => nodeMap[e.from] && nodeMap[e.to]).map(e => ({
    source: e.from,
    target: e.to,
    type: e.type,
    metadata: e.metadata || {},
    color: edgeColors[e.type] || "rgba(255,255,255,0.05)"
  }));

  return { nodes: nodesArray, edges: validEdges };
}
```

---

## Package Clustering

To create the visual clusters (like in the GitNexus screenshot), add a custom clustering force:

```javascript
// Group nodes by package
const packages = {};
nodes.forEach(n => {
  if (!packages[n.package]) packages[n.package] = [];
  packages[n.package].push(n);
});

// Assign each package a cluster center
const packageCenters = {};
const packageNames = Object.keys(packages);
packageNames.forEach((pkg, i) => {
  const angle = (2 * Math.PI * i) / packageNames.length;
  const radius = Math.min(width, height) * 0.3;
  packageCenters[pkg] = {
    x: width / 2 + radius * Math.cos(angle),
    y: height / 2 + radius * Math.sin(angle)
  };
});

// Custom force: pull nodes toward their package center
simulation.force("cluster", d3.forceX().x(d => packageCenters[d.package]?.x || width / 2).strength(0.08));
simulation.force("clusterY", d3.forceY().y(d => packageCenters[d.package]?.y || height / 2).strength(0.08));
```

---

## File Explorer Tree Builder

```javascript
function buildFileTree(nodes) {
  const tree = {};
  nodes.forEach(n => {
    if (!n.file_path) return;
    const parts = n.file_path.split('/');
    let current = tree;
    parts.forEach((part, i) => {
      if (i === parts.length - 1) {
        // File leaf
        if (!current[part]) current[part] = { _nodes: [] };
        current[part]._nodes.push(n);
      } else {
        // Directory
        if (!current[part]) current[part] = {};
        current = current[part];
      }
    });
  });
  return tree;
}
```

Render as a collapsible tree with click-to-highlight.

---

## Definition of Done

- [ ] Single HTML file loads `graph.json` and renders an interactive force-directed graph
- [ ] Nodes colored by type, sized by connection count
- [ ] Nodes cluster by package (visual grouping)
- [ ] Zoom (mouse wheel), pan (drag), works smoothly
- [ ] Hover: highlights node + neighbors, shows tooltip
- [ ] Click: opens detail panel with full node info + incoming/outgoing edges
- [ ] Search: type to find and highlight nodes
- [ ] File explorer sidebar: shows file tree, click to highlight nodes from that file
- [ ] Filter bar: toggle node/edge types on/off (imports OFF by default)
- [ ] Handles 5000+ nodes without lag (Canvas rendering, not SVG)
- [ ] Stats display: total nodes, edges, by-type breakdown
- [ ] Keyboard shortcuts work (Escape, /, +, -, 0)
- [ ] Works with both Go and Python analyzed projects
- [ ] Dark theme matching Universe branding
