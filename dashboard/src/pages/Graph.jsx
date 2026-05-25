import { useState, useEffect, useMemo } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';

function NodeRow({ node, highlighted }) {
  const navigate = useNavigate();

  const {
    id,
    name,
    type,
    package: pkg,
    file,
    memory_count,
    skill_count,
    stale_skill,
  } = node;

  const displayName = name || id || '—';
  const nodeId = id || name;

  function handleMemoryClick(e) {
    e.stopPropagation();
    navigate(`/memory?graph_node=${encodeURIComponent(nodeId)}`);
  }

  function handleSkillClick(e) {
    e.stopPropagation();
    navigate(`/skills?graph_node=${encodeURIComponent(nodeId)}`);
  }

  return (
    <div
      className="graph-node-row"
      style={highlighted ? { background: 'rgba(43,124,201,0.08)' } : {}}
    >
      <div style={{ display: 'flex', flexDirection: 'column', flex: 1, minWidth: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span className="graph-node-name" title={displayName}>{displayName}</span>
          {stale_skill && (
            <div className="stale-indicator" title="Stale skill" />
          )}
        </div>
        <div style={{ display: 'flex', gap: 8, marginTop: 2, flexWrap: 'wrap' }}>
          {type && (
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
              type: <span style={{ color: 'var(--purple)' }}>{type}</span>
            </span>
          )}
          {pkg && (
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
              pkg: <span style={{ color: 'var(--text-muted)' }}>{pkg}</span>
            </span>
          )}
          {file && (
            <span
              className="mono"
              style={{ fontSize: 10, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 300 }}
              title={file}
            >
              {file}
            </span>
          )}
        </div>
      </div>

      <div className="graph-node-badges">
        {memory_count !== undefined && memory_count !== null && (
          <span
            className="count-badge memory"
            onClick={handleMemoryClick}
            title={`${memory_count} memory observations`}
          >
            ◎ {memory_count}
          </span>
        )}
        {skill_count !== undefined && skill_count !== null && (
          <span
            className="count-badge skill"
            onClick={handleSkillClick}
            title={`${skill_count} skills`}
          >
            ◆ {skill_count}
          </span>
        )}
      </div>
    </div>
  );
}

export default function Graph() {
  const [nodes, setNodes]         = useState([]);
  const [edges, setEdges]         = useState([]);
  const [loading, setLoading]     = useState(true);
  const [error, setError]         = useState(null);
  const [search, setSearch]       = useState('');
  const [searchParams]            = useSearchParams();
  const highlightNode             = searchParams.get('node');

  useEffect(() => {
    setLoading(true);
    Promise.all([
      fetch('/api/graph/nodes').then(r => { if (!r.ok) throw new Error(`Nodes: HTTP ${r.status}`); return r.json(); }),
      fetch('/api/graph/edges').then(r => { if (!r.ok) throw new Error(`Edges: HTTP ${r.status}`); return r.json(); }),
    ])
      .then(([nodesData, edgesData]) => {
        setNodes(nodesData?.nodes || nodesData?.items || nodesData || []);
        setEdges(edgesData?.edges || edgesData?.items || edgesData || []);
        setLoading(false);
      })
      .catch(e => {
        setError(e.message);
        setLoading(false);
      });
  }, []);

  const filteredNodes = useMemo(() => {
    if (!search.trim()) return nodes;
    const q = search.toLowerCase();
    return nodes.filter(n =>
      (n.name || '').toLowerCase().includes(q) ||
      (n.id   || '').toLowerCase().includes(q) ||
      (n.type || '').toLowerCase().includes(q) ||
      (n.file || '').toLowerCase().includes(q)
    );
  }, [nodes, search]);

  const typeGroups = useMemo(() => {
    const counts = {};
    nodes.forEach(n => {
      const t = n.type || 'unknown';
      counts[t] = (counts[t] || 0) + 1;
    });
    return counts;
  }, [nodes]);

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Graph</div>
        <div className="page-subtitle">
          Code graph nodes and edges &mdash; {nodes.length} nodes, {edges.length} edges
        </div>
      </div>

      {/* Type summary pills */}
      {!loading && !error && Object.keys(typeGroups).length > 0 && (
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 16 }}>
          {Object.entries(typeGroups).map(([type, count]) => (
            <span
              key={type}
              style={{
                padding: '3px 10px',
                borderRadius: 999,
                fontSize: 11,
                background: 'rgba(83,74,183,0.12)',
                color: '#8b85e0',
                cursor: 'pointer',
              }}
              onClick={() => setSearch(type)}
            >
              {type} ({count})
            </span>
          ))}
          {search && (
            <button
              style={{
                padding: '3px 10px',
                borderRadius: 999,
                fontSize: 11,
                background: 'transparent',
                border: '1px solid var(--border)',
                color: 'var(--text-muted)',
                cursor: 'pointer',
              }}
              onClick={() => setSearch('')}
            >
              Clear filter ×
            </button>
          )}
        </div>
      )}

      <div className="search-bar">
        <input
          className="search-input"
          type="text"
          placeholder="Search nodes by name, type, file..."
          value={search}
          onChange={e => setSearch(e.target.value)}
        />
      </div>

      {loading && <div className="loading-state">Loading graph...</div>}
      {error   && <div className="error-state"><span>Failed to load graph</span><span style={{ fontSize: 12 }}>{error}</span></div>}

      {!loading && !error && (
        <>
          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 10 }}>
            Showing {filteredNodes.length} of {nodes.length} nodes
          </div>

          {filteredNodes.length === 0 ? (
            <div className="empty-state">No nodes match your search</div>
          ) : (
            <div className="data-list">
              {filteredNodes.map((node, i) => {
                const key = node.id ?? node.name ?? i;
                const isHighlighted = highlightNode && (node.id === highlightNode || node.name === highlightNode);
                return (
                  <NodeRow key={key} node={node} highlighted={!!isHighlighted} />
                );
              })}
            </div>
          )}
        </>
      )}
    </div>
  );
}
