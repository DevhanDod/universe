import { useState, useEffect, useCallback } from 'react';
import Filters from '../components/Filters.jsx';
import MetricCard from '../components/MetricCard.jsx';
import ObservationRow from '../components/ObservationRow.jsx';

const PAGE_SIZE = 20;

const FILTER_FIELDS = [
  { name: 'developer', label: 'Developer', type: 'text', placeholder: 'Filter by dev...' },
  {
    name: 'category',
    label: 'Category',
    type: 'pills',
    options: [
      { value: 'fix',      label: 'Fix' },
      { value: 'captured', label: 'Captured' },
      { value: 'derived',  label: 'Derived' },
      { value: 'manual',   label: 'Manual' },
    ],
  },
  { name: 'graph_node', label: 'Graph Node', type: 'text', placeholder: 'Node ID...' },
  { name: 'date_from',  label: 'From', type: 'date' },
  { name: 'date_to',    label: 'To',   type: 'date' },
];

function buildQuery(filters, page) {
  const params = new URLSearchParams();
  Object.entries(filters).forEach(([k, v]) => {
    if (v) params.set(k, v);
  });
  params.set('page', page);
  params.set('limit', PAGE_SIZE);
  return params.toString();
}

export default function Memory() {
  const [filters, setFilters] = useState({});
  const [page, setPage]       = useState(1);
  const [data, setData]       = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError]     = useState(null);
  const [expanded, setExpanded] = useState(null);

  const fetchData = useCallback(() => {
    setLoading(true);
    setError(null);
    fetch(`/api/memory?${buildQuery(filters, page)}`)
      .then(r => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then(d => { setData(d); setLoading(false); })
      .catch(e => { setError(e.message); setLoading(false); });
  }, [filters, page]);

  useEffect(() => { fetchData(); }, [fetchData]);

  function handleFilterChange(newFilters) {
    setFilters(newFilters);
    setPage(1);
    setExpanded(null);
  }

  const observations = data?.observations || data?.items || data?.results || [];
  const total        = data?.total ?? observations.length;
  const stats        = data?.stats || {};
  const totalPages   = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Memory</div>
        <div className="page-subtitle">Observations, patterns, and captured knowledge</div>
      </div>

      <Filters filters={filters} onChange={handleFilterChange} fields={FILTER_FIELDS} />

      <div className="metric-grid" style={{ marginBottom: 24 }}>
        <MetricCard
          label="Total Observations"
          value={total?.toLocaleString() ?? '—'}
          color="var(--blue)"
        />
        <MetricCard
          label="Recall Hit Rate"
          value={stats.recall_hit_rate !== undefined ? (stats.recall_hit_rate * 100).toFixed(1) + '%' : '—'}
          sub="last 30 days"
          color="var(--green)"
        />
        <MetricCard
          label="Shared"
          value={stats.shared_count?.toLocaleString() ?? '—'}
          sub="cross-developer"
          color="var(--teal)"
        />
      </div>

      {loading && <div className="loading-state">Loading observations...</div>}
      {error   && <div className="error-state"><span>Failed to load memory</span><span style={{ fontSize: 12 }}>{error}</span></div>}

      {!loading && !error && (
        <>
          {observations.length === 0 ? (
            <div className="empty-state">No observations found</div>
          ) : (
            <div className="data-list">
              {observations.map((obs, i) => {
                const key = obs.id ?? obs._id ?? i;
                return (
                  <ObservationRow
                    key={key}
                    obs={obs}
                    expanded={expanded === key}
                    onToggle={() => setExpanded(expanded === key ? null : key)}
                  />
                );
              })}
            </div>
          )}

          <div className="pagination">
            <span className="pagination-info">
              Page {page} of {totalPages} &mdash; {total.toLocaleString()} total
            </span>
            <div className="pagination-btns">
              <button className="btn" onClick={() => setPage(p => p - 1)} disabled={page <= 1}>
                ← Prev
              </button>
              <button className="btn" onClick={() => setPage(p => p + 1)} disabled={page >= totalPages}>
                Next →
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
