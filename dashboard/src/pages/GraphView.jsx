import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import ForceGraph2D from 'react-force-graph-2d';
import { forceRadial } from 'd3-force-3d';

const TYPE_COLORS = {
  package:   '#7C3AED',
  file:      '#06B6D4',
  function:  '#22D3EE',
  method:    '#22D3EE',
  struct:    '#2DD4BF',
  interface: '#38BDF8',
  type:      '#A78BFA',
  variable:  '#4B5563',
  constant:  '#6B7280',
  import:    '#64748B',
  class:     '#EC4899',
  module:    '#06B6D4',
  default:   '#22D3EE',
};

const TYPE_SIZES = {
  package: 12, module: 10, file: 8, struct: 6, interface: 5,
  class: 5, function: 4, method: 3, type: 4,
  variable: 2, constant: 2, import: 2, default: 3,
};

const ALL_NODE_TYPES = ['package', 'file', 'function', 'method', 'struct', 'interface', 'type', 'variable', 'import', 'class', 'module'];

export default function GraphView() {
  const [rawNodes, setRawNodes]             = useState([]);
  const [rawLinks, setRawLinks]             = useState([]);
  const [loading, setLoading]               = useState(true);
  const [error, setError]                   = useState(null);
  const [selectedNode, setSelectedNode]     = useState(null);
  const [nodeDetail, setNodeDetail]         = useState(null);
  const [detailLoading, setDetailLoading]   = useState(false);
  const [searchTerm, setSearchTerm]         = useState('');
  const [enabledTypes, setEnabledTypes]     = useState(() => new Set(ALL_NODE_TYPES));
  const [activeTab, setActiveTab]           = useState('nodes'); // 'nodes' | 'edges'
  const [nodeSearch, setNodeSearch]         = useState('');
  const [graphDims, setGraphDims]           = useState({ width: 900, height: 500 });
  const [selectedPath, setSelectedPath]     = useState(''); // '' = whole repo
  const [openSnippets, setOpenSnippets]     = useState([]); // stack of recent code previews
  const MAX_SNIPPETS = 3;

  const graphRef    = useRef();
  const canvasWrap  = useRef();
  const didInitialFit = useRef(false);
  const pinnedNodeRef = useRef(null);

  const unpinCurrent = useCallback(() => {
    const p = pinnedNodeRef.current;
    if (p) { delete p.fx; delete p.fy; }
    pinnedNodeRef.current = null;
  }, []);

  // ── fetch ──────────────────────────────────────────────────────────────────
  useEffect(() => {
    async function load() {
      try {
        setLoading(true);
        const [nr, er] = await Promise.all([
          fetch('/api/graph/nodes'),
          fetch('/api/graph/edges'),
        ]);
        const nd = await nr.json();
        const ed = await er.json();

        const nodes = (nd.nodes || []).map(n => ({
          id:          n.id,
          name:        n.name,
          type:        n.kind || n.type || 'default',
          package:     n.package || '',
          filePath:    n.file || n.file_path || '',
          startLine:   n.start_line,
          endLine:     n.end_line,
          memoryCount: n.memory_count || 0,
          skillCount:  n.skill_count  || 0,
          color: TYPE_COLORS[n.kind || n.type] || TYPE_COLORS.default,
          size:  TYPE_SIZES[n.kind  || n.type] || TYPE_SIZES.default,
        }));

        const nodeIds = new Set(nodes.map(n => n.id));
        const links = (ed.edges || [])
          .filter(e => nodeIds.has(e.source || e.from) && nodeIds.has(e.target || e.to))
          .map(e => ({
            source: e.source || e.from,
            target: e.target || e.to,
            type:   e.type   || e.label || 'unknown',
          }));

        setRawNodes(nodes);
        setRawLinks(links);
      } catch (err) {
        setError(err.message);
      } finally {
        setLoading(false);
      }
    }
    load();
  }, []);

  // ── measure canvas container ───────────────────────────────────────────────
  useEffect(() => {
    if (!canvasWrap.current) return;
    const obs = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      if (width > 0 && height > 0) setGraphDims({ width, height });
    });
    obs.observe(canvasWrap.current);
    // initial measure
    const r = canvasWrap.current.getBoundingClientRect();
    if (r.width > 0) setGraphDims({ width: r.width, height: r.height });
    return () => obs.disconnect();
  }, [loading]); // re-run once data is loaded so DOM is ready

  // ── circular layout: radial force keeps the graph inside a soft disk ───────
  // Without this the graph can sprawl asymmetrically. forceRadial pulls every
  // node toward a ring whose radius scales with node count, giving the
  // overall layout that rounded "galaxy" look.
  useEffect(() => {
    const fg = graphRef.current;
    if (!fg || rawNodes.length === 0) return;
    // Radius grows with sqrt(N) so denser graphs get larger circles.
    const radius = Math.max(120, Math.sqrt(rawNodes.length) * 18);
    fg.d3Force('radial', forceRadial(radius, 0, 0).strength(0.08));
    // Tighter charge + slightly shorter links so clusters group instead of
    // sprawling. These pair with the radial force for a tidy round shape.
    const charge = fg.d3Force('charge');
    if (charge && typeof charge.strength === 'function') {
      charge.strength(-35);
    }
    const link = fg.d3Force('link');
    if (link && typeof link.distance === 'function') {
      link.distance(28);
    }
    // Re-heat so the forces re-cool with the new constraints.
    didInitialFit.current = false;
    fg.d3ReheatSimulation();
  }, [rawNodes.length]);

  // ── filtered graph data ────────────────────────────────────────────────────
  const { fNodes, fLinks } = useMemo(() => {
    let nodes = rawNodes.filter(n => enabledTypes.has(n.type));

    // Filter by selected path from the FILES tree (folder prefix or exact file).
    if (selectedPath) {
      nodes = nodes.filter(n =>
        n.filePath === selectedPath ||
        n.filePath.startsWith(selectedPath + '/')
      );
    }

    if (searchTerm.trim()) {
      const q = searchTerm.toLowerCase();
      const matched = new Set(
        nodes.filter(n =>
          n.name.toLowerCase().includes(q) ||
          n.package.toLowerCase().includes(q) ||
          n.id.toLowerCase().includes(q)
        ).map(n => n.id)
      );
      // include direct neighbours
      rawLinks.forEach(l => {
        const s = l.source?.id || l.source;
        const t = l.target?.id || l.target;
        if (matched.has(s)) matched.add(t);
        if (matched.has(t)) matched.add(s);
      });
      nodes = nodes.filter(n => matched.has(n.id));
    }

    const ids = new Set(nodes.map(n => n.id));
    const links = rawLinks.filter(l => {
      const s = l.source?.id || l.source;
      const t = l.target?.id || l.target;
      return ids.has(s) && ids.has(t);
    });

    return { fNodes: nodes, fLinks: links };
  }, [rawNodes, rawLinks, enabledTypes, searchTerm, selectedPath]);

  // ── toggle node type ───────────────────────────────────────────────────────
  const toggleType = useCallback(type => {
    setEnabledTypes(prev => {
      const next = new Set(prev);
      next.has(type) ? next.delete(type) : next.add(type);
      return next;
    });
  }, []);

  // ── focus camera on a target point ─────────────────────────────────────────
  // The detail panel now shares space with the canvas via flex layout, so
  // ResizeObserver measures the canvas's *visible* width. centerAt(x,y) lands
  // the target at the visible center — no offset math needed.
  // Pan duration is scaled to screen-space distance so deep-zoom long pans
  // don't feel like teleports.
  const focusOn = useCallback((x, y) => {
    const fg = graphRef.current;
    if (!fg || !Number.isFinite(x) || !Number.isFinite(y)) return;
    const currentZoom = (typeof fg.zoom === 'function' && fg.zoom()) || 1;
    const center = (typeof fg.centerAt === 'function' && fg.centerAt()) || { x: 0, y: 0 };
    const dx = (x - (center.x ?? 0)) * currentZoom;
    const dy = (y - (center.y ?? 0)) * currentZoom;
    const screenDist = Math.sqrt(dx * dx + dy * dy);
    const duration = Math.max(400, Math.min(1200, screenDist * 1.5));
    fg.centerAt(x, y, duration);
  }, []);

  // ── node click ─────────────────────────────────────────────────────────────
  const handleNodeClick = useCallback(async node => {
    setSelectedNode(node);
    setNodeDetail(null);
    // Pin the clicked node so the still-running simulation doesn't drift it
    // out from under the camera during the centering animation.
    if (pinnedNodeRef.current && pinnedNodeRef.current !== node) {
      delete pinnedNodeRef.current.fx;
      delete pinnedNodeRef.current.fy;
    }
    if (Number.isFinite(node.x) && Number.isFinite(node.y)) {
      node.fx = node.x;
      node.fy = node.y;
      pinnedNodeRef.current = node;
    }
    focusOn(node.x, node.y);
    try {
      setDetailLoading(true);
      const res = await fetch(`/api/graph/node/${encodeURIComponent(node.id)}`);
      if (res.ok) {
        const d = await res.json();
        setNodeDetail(d);
        // Push to the Code Inspector stack if we got real source.
        if (d?.source_preview) {
          setOpenSnippets(prev => {
            // Deduplicate by node id — re-clicking just bumps it to the top.
            const filtered = prev.filter(s => s.nodeId !== node.id);
            const snip = {
              nodeId:        node.id,
              name:          node.name,
              type:          node.type,
              filePath:      node.filePath || d.node?.file || '',
              startLine:     d.start_line,
              endLine:       d.end_line,
              source:        d.source_preview,
              language:      d.language,
            };
            return [snip, ...filtered].slice(0, MAX_SNIPPETS);
          });
        }
      }
    } catch (_) {
      // swallow — panel will still show summary
    } finally {
      setDetailLoading(false);
    }
  }, []);

  const closeSnippet = useCallback((nodeId) => {
    setOpenSnippets(prev => prev.filter(s => s.nodeId !== nodeId));
  }, []);

  // ── paint node ─────────────────────────────────────────────────────────────
  // ── highlight set: neighbour nodes of the selected node ────────────────────
  const neighbourIds = useMemo(() => {
    if (!selectedNode) return null;
    const set = new Set([selectedNode.id]);
    rawLinks.forEach(l => {
      const s = l.source?.id || l.source;
      const t = l.target?.id || l.target;
      if (s === selectedNode.id) set.add(t);
      if (t === selectedNode.id) set.add(s);
    });
    return set;
  }, [selectedNode, rawLinks]);

  const isIncident = useCallback((link) => {
    if (!selectedNode) return false;
    const s = link.source?.id || link.source;
    const t = link.target?.id || link.target;
    return s === selectedNode.id || t === selectedNode.id;
  }, [selectedNode]);

  const paintNode = useCallback((node, ctx, globalScale) => {
    if (!Number.isFinite(node.x) || !Number.isFinite(node.y)) return;
    const size  = node.size || 3;
    const color = TYPE_COLORS[node.type] || TYPE_COLORS.default;
    const isSel = selectedNode?.id === node.id;
    // When a node is selected, dim everything outside its neighbour set so the
    // incident edges (drawn brighter via linkColor) read clearly.
    const isNeighbour = !neighbourIds || neighbourIds.has(node.id);
    const alphaPrev = ctx.globalAlpha;
    ctx.globalAlpha = isNeighbour ? 1 : 0.4;

    // glow
    if (size >= 4) {
      const g = ctx.createRadialGradient(node.x, node.y, size * 0.3, node.x, node.y, size * 2.8);
      g.addColorStop(0, color + '50');
      g.addColorStop(1, color + '00');
      ctx.beginPath();
      ctx.arc(node.x, node.y, size * 2.8, 0, 2 * Math.PI);
      ctx.fillStyle = g;
      ctx.fill();
    }

    // circle
    ctx.beginPath();
    ctx.arc(node.x, node.y, size, 0, 2 * Math.PI);
    ctx.fillStyle = isSel ? '#fff' : color;
    ctx.fill();

    if (isSel) {
      ctx.beginPath();
      ctx.arc(node.x, node.y, size + 3, 0, 2 * Math.PI);
      ctx.strokeStyle = '#F59E0B';
      ctx.lineWidth = 1.5;
      ctx.stroke();
    }

    if (globalScale > 2) {
      const fs = Math.max(9 / globalScale, 2);
      ctx.font = `${fs}px sans-serif`;
      ctx.textAlign = 'center';
      ctx.textBaseline = 'top';
      ctx.fillStyle = 'rgba(255,255,255,0.8)';
      ctx.fillText(node.name, node.x, node.y + size + 1);
    }

    ctx.globalAlpha = alphaPrev;
  }, [selectedNode, neighbourIds]);

  // ── node table filter ──────────────────────────────────────────────────────
  const tableNodes = useMemo(() => {
    if (!nodeSearch.trim()) return fNodes;
    const q = nodeSearch.toLowerCase();
    return fNodes.filter(n =>
      n.name.toLowerCase().includes(q) ||
      n.type.toLowerCase().includes(q) ||
      n.package.toLowerCase().includes(q)
    );
  }, [fNodes, nodeSearch]);

  const tableEdges = useMemo(() => {
    if (!nodeSearch.trim()) return fLinks;
    const q = nodeSearch.toLowerCase();
    return fLinks.filter(l => {
      const s = l.source?.id || l.source || '';
      const t = l.target?.id || l.target || '';
      return s.toLowerCase().includes(q) || t.toLowerCase().includes(q) || l.type.toLowerCase().includes(q);
    });
  }, [fLinks, nodeSearch]);

  // ── present types ──────────────────────────────────────────────────────────
  const presentTypes = useMemo(
    () => [...new Set(rawNodes.map(n => n.type))].filter(t => ALL_NODE_TYPES.includes(t)),
    [rawNodes]
  );

  // ── file tree built from node file paths ──────────────────────────────────
  // Result shape: { name, fullPath, count, children: { childName: childNode } }
  const fileTree = useMemo(() => {
    const root = { name: '', fullPath: '', count: 0, children: {} };
    rawNodes.forEach(n => {
      if (!n.filePath) return;
      const parts = n.filePath.split(/[\\/]/).filter(Boolean);
      let cur = root;
      let acc = '';
      parts.forEach((part, i) => {
        acc = acc ? `${acc}/${part}` : part;
        if (!cur.children[part]) {
          cur.children[part] = { name: part, fullPath: acc, count: 0, children: {}, isFile: i === parts.length - 1 };
        }
        cur = cur.children[part];
        cur.count += 1;
      });
    });
    return root;
  }, [rawNodes]);

  // ── guards ─────────────────────────────────────────────────────────────────
  if (loading) return <div className="loading-state">Loading graph…</div>;
  if (error)   return <div className="error-state"><span>Failed to load graph</span><span style={{ fontSize: 12 }}>{error}</span></div>;
  if (!loading && rawNodes.length === 0) return (
    <div>
      <div className="page-header">
        <div className="page-title">Knowledge Graph</div>
      </div>
      <div className="empty-state" style={{ flexDirection: 'column', gap: 12, height: '40vh' }}>
        <span style={{ fontSize: 32 }}>⬡</span>
        <span style={{ fontWeight: 600, color: 'var(--text)' }}>No graph data found</span>
        <span style={{ fontSize: 13 }}>Run the dashboard from inside a scanned repository:</span>
        <code style={{
          background: 'var(--card-bg)', border: '1px solid var(--border)', borderRadius: 6,
          padding: '8px 16px', fontSize: 13, color: 'var(--green)',
        }}>
          cd your-repo && universe dashboard --port 3001
        </code>
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          Or pass a path: <code style={{ color: 'var(--blue)' }}>universe dashboard --repo /path/to/repo</code>
        </span>
      </div>
    </div>
  );

  return (
    <div>
      {/* ── PAGE HEADER ─────────────────────────────────────────────────── */}
      <div className="page-header">
        <div className="page-title">Knowledge Graph</div>
        <div className="page-subtitle">
          {rawNodes.length} nodes · {rawLinks.length} edges
          {fNodes.length !== rawNodes.length && ` (showing ${fNodes.length} / ${fLinks.length})`}
        </div>
      </div>

      {/* ── TOP TOOLBAR (search + actions) ──────────────────────────────── */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: 8,
        marginBottom: 12,
      }}>
        <input
          type="text"
          placeholder="🔎  Search nodes…"
          value={searchTerm}
          onChange={e => setSearchTerm(e.target.value)}
          className="search-input"
          style={{ width: 260, marginBottom: 0 }}
        />
        {selectedPath && (
          <button
            onClick={() => setSelectedPath('')}
            style={{
              padding: '3px 10px', borderRadius: 6, fontSize: 11, cursor: 'pointer',
              border: '1px solid #1a1f2e', background: 'rgba(124,58,237,0.15)', color: '#A78BFA',
            }}
            title="Clear the FILES filter"
          >
            ✕ filter: {selectedPath}
          </button>
        )}
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 6 }}>
          <button className="btn" onClick={() => graphRef.current?.zoomToFit(400)}>Fit view</button>
          <button className="btn" onClick={() => { didInitialFit.current = false; graphRef.current?.d3ReheatSimulation(); }}>Re-layout</button>
        </div>
      </div>

      {/* ── MAIN ROW: FILES tree | canvas | detail panel ─────────────────── */}
      <div style={{
        display: 'flex',
        gap: 10,
        height: '62vh',
        marginBottom: 12,
      }}>
        {/* FILES tree */}
        <div style={{
          width: 240, flex: 'none', height: '100%', overflowY: 'auto',
          background: '#0a0d14', border: '1px solid #1a1f2e', borderRadius: 8,
          padding: '8px 4px',
        }}>
          <div style={{
            fontSize: 10, color: '#4B5563', textTransform: 'uppercase',
            letterSpacing: '0.08em', fontWeight: 700, padding: '4px 10px 8px',
          }}>
            Files
          </div>
          <FileTree
            root={fileTree}
            selectedPath={selectedPath}
            onPick={(p) => setSelectedPath(p === selectedPath ? '' : p)}
          />
        </div>

        {/* Code Inspector — stacked source previews for recently-clicked nodes.
            Sits between the FILES tree and the canvas; hidden when empty. */}
        {openSnippets.length > 0 && (
          <CodeInspector
            snippets={openSnippets}
            loading={detailLoading}
            onClose={closeSnippet}
            onClear={() => setOpenSnippets([])}
          />
        )}

        {/* canvas — flex:1 so it shrinks when the detail column opens; that
            way the canvas's measured width matches its *visible* width and
            centerAt() naturally lands clicked nodes in the visible center. */}
        <div ref={canvasWrap} style={{
          flex: 1, minWidth: 0, height: '100%',
          background: '#060810',
          borderRadius: 8, overflow: 'hidden', border: '1px solid #1a1f2e',
        }}>
          <ForceGraph2D
            ref={graphRef}
            width={graphDims.width}
            height={graphDims.height}
            graphData={{ nodes: fNodes, links: fLinks }}
            nodeCanvasObject={paintNode}
            nodePointerAreaPaint={(node, color, ctx) => {
              if (!Number.isFinite(node.x) || !Number.isFinite(node.y)) return;
              ctx.beginPath();
              ctx.arc(node.x, node.y, (node.size || 3) + 4, 0, 2 * Math.PI);
              ctx.fillStyle = color;
              ctx.fill();
            }}
            linkColor={(link) => {
              if (!selectedNode) return 'rgba(255,255,255,0.07)';
              return isIncident(link) ? 'rgba(245,158,11,0.85)' : 'rgba(255,255,255,0.1)';
            }}
            linkWidth={(link) => (selectedNode && isIncident(link) ? 1.8 : 0.5)}
            linkDirectionalArrowLength={(link) => (selectedNode && isIncident(link) ? 5 : 3)}
            linkDirectionalArrowColor={(link) =>
              selectedNode && isIncident(link) ? 'rgba(245,158,11,0.9)' : 'rgba(255,255,255,0.2)'
            }
            linkDirectionalArrowRelPos={1}
            onNodeClick={handleNodeClick}
            onLinkClick={(link) => {
              const sx = link.source?.x, sy = link.source?.y;
              const tx = link.target?.x, ty = link.target?.y;
              if ([sx, sy, tx, ty].every(Number.isFinite)) {
                focusOn((sx + tx) / 2, (sy + ty) / 2);
              }
            }}
            backgroundColor="#060810"
            cooldownTicks={120}
            onEngineStop={() => {
              if (didInitialFit.current) return;
              didInitialFit.current = true;
              graphRef.current?.zoomToFit(400, 40);
            }}
          />
        </div>

        {/* selected node overlay (top-right) */}
        {selectedNode && (
          <DetailPanel
            node={selectedNode}
            detail={nodeDetail}
            loading={detailLoading}
            allNodes={rawNodes}
            onClose={() => { unpinCurrent(); setSelectedNode(null); setNodeDetail(null); }}
            onPickNode={handleNodeClick}
          />
        )}
      </div>

      {/* ── BOTTOM: type-filter pills ─────────────────────────────────────── */}
      <div style={{
        display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 6,
        padding: '8px 10px', marginBottom: 24,
        background: '#0a0d14', border: '1px solid #1a1f2e', borderRadius: 8,
      }}>
        {presentTypes.map(type => {
          const on    = enabledTypes.has(type);
          const color = TYPE_COLORS[type] || '#6B7280';
          return (
            <button
              key={type}
              onClick={() => toggleType(type)}
              style={{
                display: 'flex', alignItems: 'center', gap: 4,
                padding: '4px 12px', borderRadius: 6, fontSize: 11, cursor: 'pointer',
                border: `1px solid ${on ? color + '80' : '#1a1f2e'}`,
                background: on ? color + '20' : 'transparent',
                color: on ? color : '#4B5563',
                fontWeight: on ? 600 : 400,
              }}
            >
              {on ? '✓' : '○'} {type}
            </button>
          );
        })}
      </div>

      {/* ── NODES & EDGES TABLE ──────────────────────────────────────────── */}
      <div className="card">
        {/* tab + search */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 14 }}>
          {['nodes', 'edges'].map(tab => (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              style={{
                padding: '5px 14px', borderRadius: 6, fontSize: 12, cursor: 'pointer',
                background: activeTab === tab ? 'rgba(43,124,201,0.15)' : 'transparent',
                border: `1px solid ${activeTab === tab ? '#2B7CC9' : '#1a1f2e'}`,
                color: activeTab === tab ? '#2B7CC9' : '#6B7280',
              }}
            >
              {tab === 'nodes'
                ? `Nodes (${fNodes.length})`
                : `Edges (${fLinks.length})`}
            </button>
          ))}

          <input
            type="text"
            placeholder={`Filter ${activeTab}…`}
            value={nodeSearch}
            onChange={e => setNodeSearch(e.target.value)}
            style={{
              marginLeft: 'auto', padding: '5px 10px', fontSize: 12, borderRadius: 6,
              background: 'var(--bg)', border: '1px solid var(--border)',
              color: 'var(--text)', outline: 'none', width: 200,
            }}
          />
        </div>

        {/* nodes table */}
        {activeTab === 'nodes' && (
          <div style={{ maxHeight: 320, overflowY: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
              <thead>
                <tr style={{ borderBottom: '1px solid #1a1f2e' }}>
                  {['Name', 'Type', 'Package', 'File'].map(h => (
                    <th key={h} style={{ padding: '6px 10px', textAlign: 'left', color: '#4B5563', fontWeight: 600, fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.07em' }}>
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {tableNodes.slice(0, 200).map(node => (
                  <tr
                    key={node.id}
                    onClick={() => handleNodeClick(node)}
                    style={{
                      borderBottom: '1px solid rgba(26,31,46,0.5)',
                      cursor: 'pointer',
                      background: selectedNode?.id === node.id ? 'rgba(43,124,201,0.08)' : 'transparent',
                    }}
                    onMouseEnter={e => { if (selectedNode?.id !== node.id) e.currentTarget.style.background = 'rgba(255,255,255,0.02)'; }}
                    onMouseLeave={e => { if (selectedNode?.id !== node.id) e.currentTarget.style.background = 'transparent'; }}
                  >
                    <td style={{ padding: '6px 10px', color: '#e5e7eb', fontFamily: 'monospace', maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {node.name}
                    </td>
                    <td style={{ padding: '6px 10px' }}>
                      <span style={{
                        padding: '1px 7px', borderRadius: 4, fontSize: 10, fontWeight: 600,
                        background: (TYPE_COLORS[node.type] || '#6B7280') + '20',
                        color: TYPE_COLORS[node.type] || '#6B7280',
                      }}>
                        {node.type}
                      </span>
                    </td>
                    <td style={{ padding: '6px 10px', color: '#6B7280', maxWidth: 150, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {node.package || '—'}
                    </td>
                    <td style={{ padding: '6px 10px', color: '#4B5563', fontFamily: 'monospace', fontSize: 11, maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {node.filePath || '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {tableNodes.length > 200 && (
              <div style={{ padding: '8px 10px', color: '#4B5563', fontSize: 11, textAlign: 'center' }}>
                Showing 200 of {tableNodes.length} — use the search filter above to narrow results
              </div>
            )}
          </div>
        )}

        {/* edges table */}
        {activeTab === 'edges' && (
          <div style={{ maxHeight: 320, overflowY: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
              <thead>
                <tr style={{ borderBottom: '1px solid #1a1f2e' }}>
                  {['Source', 'Type', 'Target'].map(h => (
                    <th key={h} style={{ padding: '6px 10px', textAlign: 'left', color: '#4B5563', fontWeight: 600, fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.07em' }}>
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {tableEdges.slice(0, 200).map((link, i) => {
                  const srcId = link.source?.id || link.source;
                  const tgtId = link.target?.id || link.target;
                  const srcNode = rawNodes.find(n => n.id === srcId);
                  const tgtNode = rawNodes.find(n => n.id === tgtId);
                  return (
                    <tr
                      key={i}
                      style={{ borderBottom: '1px solid rgba(26,31,46,0.5)', cursor: 'pointer' }}
                      onClick={() => srcNode && handleNodeClick(srcNode)}
                      onMouseEnter={e => { e.currentTarget.style.background = 'rgba(255,255,255,0.02)'; }}
                      onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                    >
                      <td style={{ padding: '6px 10px', maxWidth: 200 }}>
                        <span style={{ color: TYPE_COLORS[srcNode?.type] || '#6B7280', fontFamily: 'monospace', fontSize: 11 }}>
                          {srcNode?.name || srcId}
                        </span>
                      </td>
                      <td style={{ padding: '6px 10px' }}>
                        <span style={{ padding: '1px 7px', borderRadius: 4, fontSize: 10, background: 'rgba(75,85,99,0.2)', color: '#9CA3AF' }}>
                          {link.type}
                        </span>
                      </td>
                      <td style={{ padding: '6px 10px', maxWidth: 200 }}>
                        <span style={{ color: TYPE_COLORS[tgtNode?.type] || '#6B7280', fontFamily: 'monospace', fontSize: 11 }}>
                          {tgtNode?.name || tgtId}
                        </span>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {tableEdges.length > 200 && (
              <div style={{ padding: '8px 10px', color: '#4B5563', fontSize: 11, textAlign: 'center' }}>
                Showing 200 of {tableEdges.length} — use the filter to narrow results
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// ── Detail panel ────────────────────────────────────────────────────────────
function DetailPanel({ node, detail, loading, allNodes, onClose, onPickNode }) {
  const [showSource, setShowSource] = useState(false);
  const color = TYPE_COLORS[node.type] || '#6B7280';

  // Build outgoing/incoming from detail.callees/callers; fall back to empty.
  const callees = detail?.callees || [];
  const callers = detail?.callers || [];
  const nodeById = useMemo(() => {
    const m = new Map();
    allNodes.forEach(n => m.set(n.id, n));
    return m;
  }, [allNodes]);

  const renderConn = (id) => {
    const other = nodeById.get(id);
    const c = TYPE_COLORS[other?.type] || '#6B7280';
    return (
      <div
        key={id}
        onClick={() => other && onPickNode(other)}
        title={id}
        style={{
          fontSize: 11, padding: '3px 6px', cursor: other ? 'pointer' : 'default',
          borderRadius: 4, display: 'flex', gap: 6, alignItems: 'center',
          background: 'rgba(255,255,255,0.02)', marginBottom: 2,
        }}
      >
        <span style={{ width: 6, height: 6, borderRadius: '50%', background: c, flex: 'none' }} />
        <span style={{ color: '#cbd5e1', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {other?.name || id}
        </span>
      </div>
    );
  };

  return (
    <div style={{
      width: 340, flex: 'none', height: '100%',
      overflowY: 'auto', boxSizing: 'border-box',
      background: 'rgba(10,12,16,0.96)', border: '1px solid #1a1f2e',
      borderRadius: 8, padding: 14, fontSize: 12, backdropFilter: 'blur(6px)',
    }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
        <span style={{
          fontSize: 10, padding: '2px 7px', borderRadius: 4, fontWeight: 600,
          background: color + '25', color,
        }}>
          {node.type}
        </span>
        <button onClick={onClose} style={{ background: 'none', border: 'none', color: '#6B7280', cursor: 'pointer', fontSize: 16, lineHeight: 1 }}>×</button>
      </div>

      <div style={{ fontWeight: 600, fontSize: 14, color: '#e5e7eb', marginBottom: 8, wordBreak: 'break-word' }}>
        {node.name}
      </div>

      <div style={{ color: '#6B7280', lineHeight: 1.8, fontSize: 11, marginBottom: 10 }}>
        {node.package && <div><strong style={{ color: '#9CA3AF' }}>Package:</strong> {node.package}</div>}
        {node.filePath && <div style={{ wordBreak: 'break-all' }}><strong style={{ color: '#9CA3AF' }}>File:</strong> {node.filePath}</div>}
        {detail?.start_line > 0 && (
          <div><strong style={{ color: '#9CA3AF' }}>Lines:</strong> {detail.start_line}–{detail.end_line}</div>
        )}
        {detail?.language && <div><strong style={{ color: '#9CA3AF' }}>Language:</strong> {detail.language}</div>}
      </div>

      {detail?.signature && (
        <Section title="Signature">
          <pre style={{
            margin: 0, padding: '8px 10px', background: '#060810',
            border: '1px solid #1a1f2e', borderRadius: 6,
            fontSize: 11, color: '#a7f3d0', overflowX: 'auto', whiteSpace: 'pre-wrap', wordBreak: 'break-word',
          }}>{detail.signature}</pre>
        </Section>
      )}

      {detail?.metadata && Object.keys(detail.metadata).length > 0 && (
        <Section title="Metadata">
          <pre style={{
            margin: 0, padding: '8px 10px', background: '#060810',
            border: '1px solid #1a1f2e', borderRadius: 6,
            fontSize: 11, color: '#fde68a', overflowX: 'auto',
          }}>{JSON.stringify(detail.metadata, null, 2)}</pre>
        </Section>
      )}

      <Section title={`Outgoing (${callees.length})`} dimWhenEmpty>
        {callees.length === 0
          ? <div style={{ color: '#374151', fontSize: 11 }}>—</div>
          : callees.slice(0, 20).map(renderConn)}
      </Section>

      <Section title={`Incoming (${callers.length})`} dimWhenEmpty>
        {callers.length === 0
          ? <div style={{ color: '#374151', fontSize: 11 }}>—</div>
          : callers.slice(0, 20).map(renderConn)}
      </Section>

      {detail?.source_preview && (
        <Section title="Source Code">
          <button
            onClick={() => setShowSource(s => !s)}
            style={{
              padding: '4px 10px', fontSize: 11, borderRadius: 4, cursor: 'pointer',
              background: 'transparent', border: '1px solid #1a1f2e', color: '#9CA3AF',
              marginBottom: 6,
            }}
          >
            {showSource ? 'Hide' : 'Show'} ({(detail.source_preview.match(/\n/g) || []).length + 1} lines)
          </button>
          {showSource && (
            <pre style={{
              margin: 0, padding: '8px 10px', background: '#060810',
              border: '1px solid #1a1f2e', borderRadius: 6,
              fontSize: 11, color: '#e5e7eb', overflowX: 'auto', maxHeight: 240, overflowY: 'auto',
              lineHeight: 1.45,
            }}>{detail.source_preview}</pre>
          )}
        </Section>
      )}

      {loading && (
        <div style={{ color: '#4B5563', fontSize: 11, marginTop: 8, textAlign: 'center' }}>
          Loading details…
        </div>
      )}
    </div>
  );
}

// ── Code Inspector ──────────────────────────────────────────────────────────
function CodeInspector({ snippets, loading, onClose, onClear }) {
  return (
    <div style={{
      width: 420, flex: 'none', height: '100%',
      display: 'flex', flexDirection: 'column',
      background: '#0a0d14', border: '1px solid #1a1f2e', borderRadius: 8,
      overflow: 'hidden',
    }}>
      {/* header */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: 6,
        padding: '8px 12px', borderBottom: '1px solid #1a1f2e',
        background: 'rgba(20,24,36,0.6)',
      }}>
        <span style={{ color: '#7C3AED', fontSize: 12 }}>◇</span>
        <span style={{
          fontSize: 11, fontWeight: 700, color: '#cbd5e1',
          textTransform: 'uppercase', letterSpacing: '0.06em',
        }}>
          Code Inspector
        </span>
        <span style={{ fontSize: 10, color: '#4B5563' }}>
          {snippets.length} {snippets.length === 1 ? 'snippet' : 'snippets'}
        </span>
        {loading && (
          <span style={{ fontSize: 10, color: '#A78BFA' }}>loading…</span>
        )}
        <button
          onClick={onClear}
          title="Close all"
          style={{
            marginLeft: 'auto',
            padding: '2px 8px', borderRadius: 4, fontSize: 10, cursor: 'pointer',
            background: 'transparent', border: '1px solid #1a1f2e', color: '#6B7280',
          }}
        >
          clear
        </button>
      </div>

      {/* stacked cards */}
      <div style={{ flex: 1, overflowY: 'auto', padding: 8 }}>
        {snippets.map(s => (
          <CodeCard key={s.nodeId} snippet={s} onClose={() => onClose(s.nodeId)} />
        ))}
      </div>
    </div>
  );
}

function CodeCard({ snippet, onClose }) {
  const lines = (snippet.source || '').split('\n');
  const startLine = snippet.startLine || 1;
  const typeColor = TYPE_COLORS[snippet.type] || TYPE_COLORS.default;

  return (
    <div style={{
      marginBottom: 10,
      background: '#060810', border: '1px solid #1a1f2e', borderRadius: 6,
      overflow: 'hidden',
    }}>
      {/* card header */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: 6,
        padding: '6px 10px', borderBottom: '1px solid #1a1f2e',
        background: 'rgba(20,24,36,0.4)',
      }}>
        <span style={{
          fontSize: 9, padding: '1px 6px', borderRadius: 3, fontWeight: 600,
          background: typeColor + '25', color: typeColor,
        }}>
          {snippet.type}
        </span>
        <span style={{
          fontSize: 12, fontWeight: 600, color: '#e5e7eb', fontFamily: 'monospace',
          overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
        }}>
          {snippet.name}
        </span>
        <span style={{ fontSize: 10, color: '#4B5563' }}>
          {snippet.startLine}–{snippet.endLine}
        </span>
        <button
          onClick={onClose}
          title="Close"
          style={{
            marginLeft: 'auto',
            background: 'none', border: 'none', cursor: 'pointer',
            color: '#6B7280', fontSize: 14, lineHeight: 1,
          }}
        >×</button>
      </div>

      {/* file path */}
      {snippet.filePath && (
        <div style={{
          padding: '3px 10px', borderBottom: '1px solid #1a1f2e',
          fontSize: 10, color: '#6B7280', fontFamily: 'monospace',
          overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
        }}>
          {snippet.filePath}
        </div>
      )}

      {/* code with line numbers */}
      <div style={{
        display: 'flex',
        fontFamily: '"SF Mono", Menlo, Consolas, monospace',
        fontSize: 11, lineHeight: '1.55',
        maxHeight: 280, overflow: 'auto',
      }}>
        {/* gutter */}
        <div style={{
          padding: '6px 8px 6px 10px', textAlign: 'right',
          color: '#374151', background: '#0a0d14',
          borderRight: '1px solid #1a1f2e', userSelect: 'none',
          flex: 'none', minWidth: 36,
        }}>
          {lines.map((_, i) => (
            <div key={i}>{startLine + i}</div>
          ))}
        </div>
        {/* code — every word-bounded occurrence of the clicked node's name
            gets highlighted, so it's easy to spot the symbol amongst its
            siblings (other imports, other fields, other defs in the slice). */}
        <pre style={{
          margin: 0, padding: '6px 10px', flex: 1,
          color: '#e5e7eb', whiteSpace: 'pre',
          overflow: 'visible',
        }}>
          {highlightOccurrences(snippet.source, snippet.name)}
        </pre>
      </div>
    </div>
  );
}

// Renders `source` as React children, wrapping every word-bounded occurrence
// of `name` in a highlighted <mark>. Falls back to plain text when name is
// empty or matches nothing. Uses word boundaries so "User" doesn't match
// inside "UserService".
function highlightOccurrences(source, name) {
  if (!source) return '';
  if (!name || name === '<embedded>') return source;
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const re = new RegExp(`\\b${escaped}\\b`, 'g');
  const out = [];
  let last = 0;
  let m;
  let i = 0;
  while ((m = re.exec(source)) !== null) {
    if (m.index > last) out.push(source.slice(last, m.index));
    out.push(
      <mark
        key={`h-${i++}`}
        style={{
          background: 'rgba(245,158,11,0.28)',
          color: '#fde68a',
          borderRadius: 2,
          padding: '0 1px',
        }}
      >
        {m[0]}
      </mark>
    );
    last = m.index + m[0].length;
    // guard against zero-width matches
    if (re.lastIndex === m.index) re.lastIndex++;
  }
  if (last < source.length) out.push(source.slice(last));
  return out.length > 0 ? out : source;
}

// ── FileTree ────────────────────────────────────────────────────────────────
function FileTree({ root, selectedPath, onPick }) {
  // Render the top-level children (skip the empty-named root itself).
  const entries = Object.values(root.children).sort((a, b) => {
    // folders (with children) first, then files; alphabetical within.
    const aIsFolder = Object.keys(a.children).length > 0;
    const bIsFolder = Object.keys(b.children).length > 0;
    if (aIsFolder !== bIsFolder) return aIsFolder ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
  return (
    <div>
      {entries.map(entry => (
        <TreeRow
          key={entry.fullPath}
          node={entry}
          depth={0}
          selectedPath={selectedPath}
          onPick={onPick}
        />
      ))}
    </div>
  );
}

function TreeRow({ node, depth, selectedPath, onPick }) {
  const hasChildren = Object.keys(node.children).length > 0;
  // Auto-expand the top level for visibility; deeper levels start collapsed.
  const [open, setOpen] = useState(depth < 1);
  const isSelected = selectedPath === node.fullPath;
  // Highlight ancestors of the current selection too.
  const isAncestor = selectedPath && selectedPath.startsWith(node.fullPath + '/');

  const childEntries = useMemo(() => Object.values(node.children).sort((a, b) => {
    const aF = Object.keys(a.children).length > 0;
    const bF = Object.keys(b.children).length > 0;
    if (aF !== bF) return aF ? -1 : 1;
    return a.name.localeCompare(b.name);
  }), [node.children]);

  return (
    <div>
      <div
        onClick={() => {
          if (hasChildren) setOpen(o => !o);
          onPick(node.fullPath);
        }}
        title={node.fullPath}
        style={{
          display: 'flex', alignItems: 'center', gap: 4,
          padding: '3px 6px', paddingLeft: 8 + depth * 12,
          cursor: 'pointer', fontSize: 12,
          background: isSelected ? 'rgba(124,58,237,0.18)' : (isAncestor ? 'rgba(124,58,237,0.06)' : 'transparent'),
          color: isSelected ? '#A78BFA' : (hasChildren ? '#cbd5e1' : '#9CA3AF'),
          borderLeft: isSelected ? '2px solid #A78BFA' : '2px solid transparent',
        }}
      >
        <span style={{ width: 10, display: 'inline-block', color: '#4B5563', fontSize: 10 }}>
          {hasChildren ? (open ? '▾' : '▸') : ''}
        </span>
        <span style={{
          flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
          fontWeight: hasChildren ? 500 : 400,
        }}>
          {node.name}
        </span>
        <span style={{
          fontSize: 9, color: '#4B5563', fontFamily: 'monospace',
          padding: '0 4px',
        }}>
          {node.count}
        </span>
      </div>
      {hasChildren && open && (
        <div>
          {childEntries.map(child => (
            <TreeRow
              key={child.fullPath}
              node={child}
              depth={depth + 1}
              selectedPath={selectedPath}
              onPick={onPick}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function Section({ title, children, dimWhenEmpty }) {
  return (
    <div style={{ marginTop: 10, borderTop: '1px solid #1a1f2e', paddingTop: 8 }}>
      <div style={{
        fontSize: 10, color: dimWhenEmpty ? '#374151' : '#4B5563',
        textTransform: 'uppercase', letterSpacing: '0.07em', marginBottom: 6, fontWeight: 600,
      }}>
        {title}
      </div>
      {children}
    </div>
  );
}
