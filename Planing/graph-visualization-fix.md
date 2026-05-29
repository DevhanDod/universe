# Fix: Add Force-Directed Graph Visualization to Dashboard

## Problem

The dashboard Graph page loads data from `/api/graph/nodes` (504 nodes, 2669 edges) but doesn't render it visually. The page is empty or shows a list. We need an interactive force-directed graph like a network visualization.

## Solution

Add `react-force-graph-2d` to the dashboard React app. This renders nodes as circles and edges as lines, with physics simulation that spreads them out naturally.

---

## Step 1: Install the library

```bash
cd dashboard
npm install react-force-graph-2d
```

---

## Step 2: Create the Graph component

**Create `dashboard/src/pages/GraphView.jsx`:**

```jsx
import { useState, useEffect, useRef, useCallback } from 'react';
import ForceGraph2D from 'react-force-graph-2d';

// Color map for different node types
const NODE_COLORS = {
  file:      '#3B82F6',  // blue
  function:  '#10B981',  // green
  method:    '#8B5CF6',  // purple
  struct:    '#F59E0B',  // amber
  interface: '#EF4444',  // red
  type:      '#EC4899',  // pink
  package:   '#06B6D4',  // cyan
  variable:  '#6B7280',  // gray
  constant:  '#F97316',  // orange
  default:   '#6B7280',  // gray
};

// Size map — bigger for important node types
const NODE_SIZES = {
  package:   8,
  file:      5,
  struct:    6,
  interface: 6,
  function:  4,
  method:    3,
  type:      4,
  variable:  2,
  constant:  2,
  default:   3,
};

export default function GraphView() {
  const [graphData, setGraphData] = useState({ nodes: [], links: [] });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [selectedNode, setSelectedNode] = useState(null);
  const [searchTerm, setSearchTerm] = useState('');
  const [filterType, setFilterType] = useState('all');
  const [stats, setStats] = useState({ nodes: 0, edges: 0, packages: 0 });
  const graphRef = useRef();

  // Fetch graph data from the API
  useEffect(() => {
    async function loadGraph() {
      try {
        setLoading(true);

        // Fetch nodes and edges
        const [nodesRes, edgesRes] = await Promise.all([
          fetch('/api/graph/nodes'),
          fetch('/api/graph/edges'),
        ]);

        const nodesData = await nodesRes.json();
        const edgesData = await edgesRes.json();

        const nodes = (nodesData.nodes || []).map(node => ({
          id: node.id,
          name: node.name,
          type: node.type || 'default',
          package: node.package || '',
          filePath: node.file_path || '',
          startLine: node.start_line,
          endLine: node.end_line,
          color: NODE_COLORS[node.type] || NODE_COLORS.default,
          size: NODE_SIZES[node.type] || NODE_SIZES.default,
        }));

        // Build a set of valid node IDs for filtering edges
        const nodeIds = new Set(nodes.map(n => n.id));

        const links = (edgesData.edges || [])
          .filter(edge => {
            // Only include edges where both source and target exist
            const source = edge.source || edge.from;
            const target = edge.target || edge.to;
            return nodeIds.has(source) && nodeIds.has(target);
          })
          .map(edge => ({
            source: edge.source || edge.from,
            target: edge.target || edge.to,
            type: edge.type || edge.label || 'depends',
          }));

        // Calculate stats
        const packages = new Set(nodes.map(n => n.package).filter(Boolean));

        setStats({
          nodes: nodes.length,
          edges: links.length,
          packages: packages.size,
        });

        setGraphData({ nodes, links });
        setLoading(false);
      } catch (err) {
        setError(err.message);
        setLoading(false);
      }
    }

    loadGraph();
  }, []);

  // Filter nodes based on search and type filter
  const filteredData = useCallback(() => {
    let nodes = graphData.nodes;
    let links = graphData.links;

    // Apply type filter
    if (filterType !== 'all') {
      const filteredNodeIds = new Set(
        nodes.filter(n => n.type === filterType).map(n => n.id)
      );
      nodes = nodes.filter(n => filteredNodeIds.has(n.id));
      links = links.filter(
        l => filteredNodeIds.has(l.source?.id || l.source) &&
             filteredNodeIds.has(l.target?.id || l.target)
      );
    }

    // Apply search filter
    if (searchTerm) {
      const term = searchTerm.toLowerCase();
      const matchedIds = new Set(
        nodes
          .filter(n =>
            n.name.toLowerCase().includes(term) ||
            n.package.toLowerCase().includes(term) ||
            n.id.toLowerCase().includes(term)
          )
          .map(n => n.id)
      );

      // Include matched nodes AND their direct neighbors
      const neighborIds = new Set(matchedIds);
      links.forEach(l => {
        const sourceId = l.source?.id || l.source;
        const targetId = l.target?.id || l.target;
        if (matchedIds.has(sourceId)) neighborIds.add(targetId);
        if (matchedIds.has(targetId)) neighborIds.add(sourceId);
      });

      nodes = nodes.filter(n => neighborIds.has(n.id));
      links = links.filter(
        l => neighborIds.has(l.source?.id || l.source) &&
             neighborIds.has(l.target?.id || l.target)
      );
    }

    return { nodes, links };
  }, [graphData, filterType, searchTerm]);

  // Get unique node types for the filter dropdown
  const nodeTypes = [...new Set(graphData.nodes.map(n => n.type))].sort();

  // Handle node click
  const handleNodeClick = useCallback((node) => {
    setSelectedNode(node);
    // Center the view on the clicked node
    if (graphRef.current) {
      graphRef.current.centerAt(node.x, node.y, 500);
      graphRef.current.zoom(3, 500);
    }
  }, []);

  // Custom node rendering
  const paintNode = useCallback((node, ctx, globalScale) => {
    const size = node.size || 3;
    const fontSize = Math.max(10 / globalScale, 1);
    const isSelected = selectedNode?.id === node.id;

    // Draw node circle
    ctx.beginPath();
    ctx.arc(node.x, node.y, size, 0, 2 * Math.PI);
    ctx.fillStyle = isSelected ? '#FFFFFF' : (node.color || '#6B7280');
    ctx.fill();

    // Draw border for selected node
    if (isSelected) {
      ctx.strokeStyle = '#F59E0B';
      ctx.lineWidth = 2;
      ctx.stroke();
    }

    // Draw label when zoomed in enough
    if (globalScale > 1.5) {
      ctx.font = `${fontSize}px Inter, sans-serif`;
      ctx.textAlign = 'center';
      ctx.textBaseline = 'top';
      ctx.fillStyle = 'rgba(255, 255, 255, 0.8)';
      ctx.fillText(node.name, node.x, node.y + size + 2);
    }
  }, [selectedNode]);

  if (loading) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '80vh', color: '#6b7280' }}>
        Loading graph...
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ padding: 20, color: '#f87171' }}>
        Error loading graph: {error}
      </div>
    );
  }

  const filtered = filteredData();

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* Header bar */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: 12,
        padding: '12px 0', borderBottom: '1px solid #1a1f2e', marginBottom: 8
      }}>
        <h2 style={{ margin: 0, fontSize: 16, fontWeight: 500 }}>Knowledge Graph</h2>

        <div style={{
          display: 'flex', gap: 8, marginLeft: 16,
          fontSize: 12, color: '#6b7280'
        }}>
          <span style={{ padding: '4px 10px', background: '#0f1117', borderRadius: 6 }}>
            {stats.nodes} nodes
          </span>
          <span style={{ padding: '4px 10px', background: '#0f1117', borderRadius: 6 }}>
            {stats.edges} edges
          </span>
          <span style={{ padding: '4px 10px', background: '#0f1117', borderRadius: 6 }}>
            {stats.packages} packages
          </span>
        </div>

        {/* Search */}
        <input
          type="text"
          placeholder="Search nodes..."
          value={searchTerm}
          onChange={e => setSearchTerm(e.target.value)}
          style={{
            marginLeft: 'auto', padding: '6px 12px', fontSize: 12,
            background: '#0f1117', border: '1px solid #1a1f2e', borderRadius: 6,
            color: '#e5e7eb', width: 200, outline: 'none',
          }}
        />

        {/* Type filter */}
        <select
          value={filterType}
          onChange={e => setFilterType(e.target.value)}
          style={{
            padding: '6px 12px', fontSize: 12,
            background: '#0f1117', border: '1px solid #1a1f2e', borderRadius: 6,
            color: '#e5e7eb', outline: 'none',
          }}
        >
          <option value="all">All types</option>
          {nodeTypes.map(t => (
            <option key={t} value={t}>{t} ({graphData.nodes.filter(n => n.type === t).length})</option>
          ))}
        </select>

        {/* Reset zoom */}
        <button
          onClick={() => graphRef.current?.zoomToFit(400)}
          style={{
            padding: '6px 12px', fontSize: 12,
            background: '#1a1f2e', border: 'none', borderRadius: 6,
            color: '#e5e7eb', cursor: 'pointer',
          }}
        >
          Reset zoom
        </button>
      </div>

      {/* Legend */}
      <div style={{ display: 'flex', gap: 12, padding: '4px 0 8px', flexWrap: 'wrap' }}>
        {Object.entries(NODE_COLORS).filter(([k]) => k !== 'default').map(([type, color]) => (
          <div key={type} style={{
            display: 'flex', alignItems: 'center', gap: 4, fontSize: 11, color: '#6b7280',
            cursor: 'pointer', opacity: filterType === 'all' || filterType === type ? 1 : 0.3,
          }} onClick={() => setFilterType(filterType === type ? 'all' : type)}>
            <div style={{ width: 8, height: 8, borderRadius: '50%', background: color }} />
            {type}
          </div>
        ))}
      </div>

      {/* Graph + Detail panel */}
      <div style={{ flex: 1, display: 'flex', position: 'relative' }}>
        {/* Force graph */}
        <div style={{ flex: 1, background: '#060810', borderRadius: 8, overflow: 'hidden' }}>
          <ForceGraph2D
            ref={graphRef}
            graphData={filtered}
            nodeCanvasObject={paintNode}
            nodePointerAreaPaint={(node, color, ctx) => {
              const size = node.size || 3;
              ctx.beginPath();
              ctx.arc(node.x, node.y, size + 2, 0, 2 * Math.PI);
              ctx.fillStyle = color;
              ctx.fill();
            }}
            linkColor={() => 'rgba(255,255,255,0.06)'}
            linkWidth={0.5}
            linkDirectionalArrowLength={3}
            linkDirectionalArrowRelPos={1}
            onNodeClick={handleNodeClick}
            backgroundColor="#060810"
            cooldownTicks={100}
            onEngineStop={() => graphRef.current?.zoomToFit(400)}
          />
        </div>

        {/* Selected node detail panel */}
        {selectedNode && (
          <div style={{
            position: 'absolute', top: 12, right: 12,
            width: 280, background: '#0f1117', borderRadius: 8,
            border: '1px solid #1a1f2e', padding: 16, fontSize: 12,
          }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
              <span style={{
                fontSize: 10, padding: '2px 6px', borderRadius: 4,
                background: NODE_COLORS[selectedNode.type] + '20',
                color: NODE_COLORS[selectedNode.type],
              }}>
                {selectedNode.type}
              </span>
              <button
                onClick={() => setSelectedNode(null)}
                style={{ background: 'none', border: 'none', color: '#6b7280', cursor: 'pointer', fontSize: 14 }}
              >×</button>
            </div>

            <h3 style={{ margin: '0 0 8px', fontSize: 14, fontWeight: 500, color: '#f9fafb' }}>
              {selectedNode.name}
            </h3>

            <div style={{ color: '#6b7280', lineHeight: 1.8 }}>
              <div><strong>ID:</strong> {selectedNode.id}</div>
              <div><strong>Package:</strong> {selectedNode.package}</div>
              <div><strong>File:</strong> {selectedNode.filePath}</div>
              {selectedNode.startLine && (
                <div><strong>Lines:</strong> {selectedNode.startLine}–{selectedNode.endLine}</div>
              )}
            </div>

            {/* Show connections */}
            <div style={{ marginTop: 12, borderTop: '1px solid #1a1f2e', paddingTop: 8 }}>
              <div style={{ fontSize: 11, color: '#6b7280', marginBottom: 4 }}>Connections:</div>
              {graphData.links
                .filter(l =>
                  (l.source?.id || l.source) === selectedNode.id ||
                  (l.target?.id || l.target) === selectedNode.id
                )
                .slice(0, 10)
                .map((l, i) => {
                  const otherId = (l.source?.id || l.source) === selectedNode.id
                    ? (l.target?.id || l.target)
                    : (l.source?.id || l.source);
                  const otherNode = graphData.nodes.find(n => n.id === otherId);
                  return (
                    <div key={i}
                      style={{ fontSize: 11, color: '#9ca3af', padding: '2px 0', cursor: 'pointer' }}
                      onClick={() => {
                        const n = graphData.nodes.find(n => n.id === otherId);
                        if (n) handleNodeClick(n);
                      }}
                    >
                      → {otherNode?.name || otherId}
                    </div>
                  );
                })
              }
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
```

---

## Step 3: Update the App.jsx route (if not already)

Make sure `GraphView` is imported and routed:

```jsx
import GraphView from './pages/GraphView';

// In Routes:
<Route path="/graph" element={<GraphView />} />
```

---

## Step 4: Build and test

```bash
cd dashboard
npm install react-force-graph-2d
npm run build
cd ..

# Rebuild the Go binary (embeds the new dashboard)
go build -ldflags "-s -w -X main.Version=0.1.4" -o universe.exe ./cmd/universe

# Copy to npm location
cp universe.exe /c/Users/dedolk/AppData/Roaming/npm/node_modules/@devhand/universe/bin/universe.exe

# Test
cd ~/OneDrive\ -\ IFS/Desktop/Elavate/nexus-platform-service-automation
universe dashboard --port 3001
# Open http://localhost:3001/graph
```

---

## What You Should See

- Dark background with 504 nodes floating as colored circles
- Lines (edges) connecting related nodes
- Different colors for different node types (functions=green, files=blue, structs=amber)
- Nodes spread out with physics simulation
- Click a node → detail panel shows name, package, file, connections
- Search bar filters nodes by name
- Type dropdown filters by node type (function, file, struct, etc.)
- Mouse wheel zooms in/out
- Drag to pan
- Click "Reset zoom" to fit all nodes in view

---

## Performance Note

504 nodes + 2669 edges should render smoothly. If it feels slow:
- The `cooldownTicks={100}` setting limits the physics simulation
- `linkWidth={0.5}` and low-opacity links reduce rendering load
- Labels only appear when zoomed in (`globalScale > 1.5`)

For very large graphs (5000+ nodes), consider:
- Only showing nodes of selected types (use the filter)
- Clustering by package
- Using `react-force-graph-3d` with WebGL for better performance
