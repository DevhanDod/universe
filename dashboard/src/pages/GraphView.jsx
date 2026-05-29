import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import ForceGraph2D from 'react-force-graph-2d';

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
  const [searchTerm, setSearchTerm]         = useState('');
  const [enabledTypes, setEnabledTypes]     = useState(() => new Set(ALL_NODE_TYPES));
  const [activeTab, setActiveTab]           = useState('nodes'); // 'nodes' | 'edges'
  const [nodeSearch, setNodeSearch]         = useState('');
  const [graphDims, setGraphDims]           = useState({ width: 900, height: 500 });

  const graphRef    = useRef();
  const canvasWrap  = useRef();

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

  // ── filtered graph data ────────────────────────────────────────────────────
  const { fNodes, fLinks } = useMemo(() => {
    let nodes = rawNodes.filter(n => enabledTypes.has(n.type));

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
  }, [rawNodes, rawLinks, enabledTypes, searchTerm]);

  // ── toggle node type ───────────────────────────────────────────────────────
  const toggleType = useCallback(type => {
    setEnabledTypes(prev => {
      const next = new Set(prev);
      next.has(type) ? next.delete(type) : next.add(type);
      return next;
    });
  }, []);

  // ── node click ─────────────────────────────────────────────────────────────
  const handleNodeClick = useCallback(node => {
    setSelectedNode(node);
    graphRef.current?.centerAt(node.x, node.y, 400);
    graphRef.current?.zoom(4, 400);
  }, []);

  // ── paint node ─────────────────────────────────────────────────────────────
  const paintNode = useCallback((node, ctx, globalScale) => {
    const size  = node.size || 3;
    const color = TYPE_COLORS[node.type] || TYPE_COLORS.default;
    const isSel = selectedNode?.id === node.id;

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
  }, [selectedNode]);

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

      {/* ── CONTROLS ─────────────────────────────────────────────────────── */}
      <div style={{
        display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8,
        marginBottom: 12,
      }}>
        {/* search */}
        <input
          type="text"
          placeholder="Search nodes…"
          value={searchTerm}
          onChange={e => setSearchTerm(e.target.value)}
          className="search-input"
          style={{ width: 220, marginBottom: 0 }}
        />

        {/* type filter pills */}
        {presentTypes.map(type => {
          const on    = enabledTypes.has(type);
          const color = TYPE_COLORS[type] || '#6B7280';
          return (
            <button
              key={type}
              onClick={() => toggleType(type)}
              style={{
                display: 'flex', alignItems: 'center', gap: 4,
                padding: '3px 10px', borderRadius: 999, fontSize: 11, cursor: 'pointer',
                border: `1px solid ${on ? color + '80' : '#1a1f2e'}`,
                background: on ? color + '20' : 'transparent',
                color: on ? color : '#374151',
              }}
            >
              {on && <span style={{ fontSize: 9 }}>✓</span>}
              {type}
            </button>
          );
        })}

        <div style={{ marginLeft: 'auto', display: 'flex', gap: 6 }}>
          <button className="btn" onClick={() => graphRef.current?.zoomToFit(400)}>Fit view</button>
          <button className="btn" onClick={() => graphRef.current?.d3ReheatSimulation()}>Re-layout</button>
        </div>
      </div>

      {/* ── GRAPH (75% height) ───────────────────────────────────────────── */}
      <div style={{
        position: 'relative',
        height: '62vh',
        borderRadius: 8,
        overflow: 'hidden',
        border: '1px solid #1a1f2e',
        marginBottom: 24,
      }}>
        {/* canvas */}
        <div ref={canvasWrap} style={{ width: '100%', height: '100%', background: '#060810' }}>
          <ForceGraph2D
            ref={graphRef}
            width={graphDims.width}
            height={graphDims.height}
            graphData={{ nodes: fNodes, links: fLinks }}
            nodeCanvasObject={paintNode}
            nodePointerAreaPaint={(node, color, ctx) => {
              ctx.beginPath();
              ctx.arc(node.x, node.y, (node.size || 3) + 4, 0, 2 * Math.PI);
              ctx.fillStyle = color;
              ctx.fill();
            }}
            linkColor={() => 'rgba(255,255,255,0.07)'}
            linkWidth={0.5}
            linkDirectionalArrowLength={3}
            linkDirectionalArrowRelPos={1}
            onNodeClick={handleNodeClick}
            backgroundColor="#060810"
            cooldownTicks={120}
            onEngineStop={() => graphRef.current?.zoomToFit(400)}
          />
        </div>

        {/* selected node overlay (top-right) */}
        {selectedNode && (
          <div style={{
            position: 'absolute', top: 10, right: 10, width: 250,
            background: 'rgba(10,12,16,0.95)', border: '1px solid #1a1f2e',
            borderRadius: 8, padding: 14, fontSize: 12, backdropFilter: 'blur(6px)',
          }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
              <span style={{
                fontSize: 10, padding: '2px 7px', borderRadius: 4, fontWeight: 600,
                background: (TYPE_COLORS[selectedNode.type] || '#6B7280') + '25',
                color: TYPE_COLORS[selectedNode.type] || '#6B7280',
              }}>
                {selectedNode.type}
              </span>
              <button onClick={() => setSelectedNode(null)} style={{ background: 'none', border: 'none', color: '#6B7280', cursor: 'pointer', fontSize: 16, lineHeight: 1 }}>×</button>
            </div>

            <div style={{ fontWeight: 600, fontSize: 13, color: '#e5e7eb', marginBottom: 8, wordBreak: 'break-word' }}>
              {selectedNode.name}
            </div>

            <div style={{ color: '#6B7280', lineHeight: 1.9, fontSize: 11 }}>
              {selectedNode.package  && <div><strong>Package:</strong> {selectedNode.package}</div>}
              {selectedNode.filePath && <div style={{ wordBreak: 'break-all' }}><strong>File:</strong> {selectedNode.filePath}</div>}
              {selectedNode.startLine && <div><strong>Lines:</strong> {selectedNode.startLine}–{selectedNode.endLine}</div>}
            </div>

            <div style={{ marginTop: 10, borderTop: '1px solid #1a1f2e', paddingTop: 8 }}>
              <div style={{ fontSize: 10, color: '#4B5563', textTransform: 'uppercase', letterSpacing: '0.07em', marginBottom: 4 }}>
                Connections
              </div>
              {rawLinks
                .filter(l =>
                  (l.source?.id || l.source) === selectedNode.id ||
                  (l.target?.id || l.target) === selectedNode.id
                )
                .slice(0, 6)
                .map((l, i) => {
                  const otherId = (l.source?.id || l.source) === selectedNode.id
                    ? (l.target?.id || l.target) : (l.source?.id || l.source);
                  const other = rawNodes.find(n => n.id === otherId);
                  return (
                    <div
                      key={i}
                      onClick={() => other && handleNodeClick(other)}
                      style={{ fontSize: 11, padding: '2px 0', cursor: 'pointer', display: 'flex', gap: 5, alignItems: 'center' }}
                    >
                      <span style={{ color: '#4B5563' }}>→</span>
                      <span style={{ color: TYPE_COLORS[other?.type] || '#6B7280', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {other?.name || otherId}
                      </span>
                      <span style={{ fontSize: 9, color: '#374151' }}>{l.type}</span>
                    </div>
                  );
                })}
            </div>
          </div>
        )}
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
