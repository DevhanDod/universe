import { useState, useEffect, useCallback } from 'react';
import Filters from '../components/Filters.jsx';
import MetricCard from '../components/MetricCard.jsx';
import Badge from '../components/Badge.jsx';
import RoutingTrace from '../components/RoutingTrace.jsx';

const FILTER_FIELDS = [
  { name: 'developer', label: 'Developer', type: 'text', placeholder: 'Filter by dev...' },
  {
    name: 'routing_mode',
    label: 'Mode',
    type: 'pills',
    options: [
      { value: 'single_haiku',      label: 'Haiku' },
      { value: 'single_opus',       label: 'Opus' },
      { value: 'full_orchestration', label: 'Full Orch.' },
      { value: 'plan_execute',      label: 'Plan+Exec' },
    ],
  },
  { name: 'date_from', label: 'From', type: 'date' },
  { name: 'date_to',   label: 'To',   type: 'date' },
];

function formatTime(ts) {
  if (!ts) return '—';
  try {
    const d = new Date(ts);
    return d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
  } catch {
    return ts;
  }
}

function fmtCurrency(n) {
  if (n === undefined || n === null) return '—';
  return '$' + Number(n).toFixed(4);
}

function fmtMs(ms) {
  if (ms === undefined || ms === null) return '—';
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function developerInitial(dev) {
  if (!dev) return '?';
  return dev.charAt(0).toUpperCase();
}

function TaskRow({ task }) {
  const [expanded, setExpanded] = useState(false);
  const [trace, setTrace]       = useState(null);
  const [loadingTrace, setLoadingTrace] = useState(false);

  const {
    id,
    timestamp,
    developer,
    prompt_preview,
    routing_mode,
    tokens,
    cost,
    latency_ms,
  } = task;

  function handleToggle() {
    if (!expanded && !trace && id) {
      setLoadingTrace(true);
      fetch(`/api/routing/${id}`)
        .then(r => r.json())
        .then(d => {
          setTrace(d?.trace || d?.steps || []);
          setLoadingTrace(false);
        })
        .catch(() => {
          setTrace([]);
          setLoadingTrace(false);
        });
    }
    setExpanded(e => !e);
  }

  const totalTokens = typeof tokens === 'object'
    ? (tokens?.input ?? 0) + (tokens?.output ?? 0)
    : tokens;

  return (
    <div className="task-row">
      <div className="task-collapsed" onClick={handleToggle}>
        <span style={{ fontSize: 11, color: 'var(--text-muted)', fontFamily: 'JetBrains Mono, monospace', flexShrink: 0 }}>
          {formatTime(timestamp)}
        </span>
        <div
          style={{
            width: 26, height: 26, borderRadius: '50%',
            background: 'rgba(43,124,201,0.18)', color: 'var(--blue)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: 11, fontWeight: 700, flexShrink: 0,
          }}
        >
          {developerInitial(developer)}
        </div>
        <span className="task-prompt">{prompt_preview || '—'}</span>
        {routing_mode && <Badge type={routing_mode} />}
        {totalTokens !== undefined && totalTokens !== null && (
          <span className="mono" style={{ fontSize: 11, color: 'var(--text-muted)', flexShrink: 0 }}>
            {typeof totalTokens === 'number' ? totalTokens.toLocaleString() : totalTokens} tok
          </span>
        )}
        {cost !== undefined && (
          <span className="mono" style={{ fontSize: 11, color: 'var(--text-muted)', flexShrink: 0 }}>
            {fmtCurrency(cost)}
          </span>
        )}
        {latency_ms !== undefined && (
          <span style={{ fontSize: 11, color: 'var(--text-muted)', flexShrink: 0 }}>
            {fmtMs(latency_ms)}
          </span>
        )}
        <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{expanded ? '▲' : '▼'}</span>
      </div>

      {expanded && (
        <div className="task-expanded">
          {loadingTrace ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading trace...</div>
          ) : (
            <RoutingTrace trace={trace || task.trace || task.steps || []} />
          )}
        </div>
      )}
    </div>
  );
}

export default function Routing() {
  const [filters, setFilters] = useState({});
  const [data, setData]       = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError]     = useState(null);

  const fetchData = useCallback(() => {
    setLoading(true);
    setError(null);
    const params = new URLSearchParams();
    Object.entries(filters).forEach(([k, v]) => { if (v) params.set(k, v); });
    fetch(`/api/routing?${params.toString()}`)
      .then(r => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then(d => { setData(d); setLoading(false); })
      .catch(e => { setError(e.message); setLoading(false); });
  }, [filters]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const tasks  = data?.tasks || data?.items || data?.results || [];
  const stats  = data?.stats || {};

  function fmtPct(n) {
    if (n === undefined || n === null) return '—';
    return Number(n).toFixed(1) + '%';
  }

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Routing</div>
        <div className="page-subtitle">Task routing decisions and model usage</div>
      </div>

      <Filters filters={filters} onChange={f => setFilters(f)} fields={FILTER_FIELDS} />

      <div className="metric-grid" style={{ marginBottom: 24 }}>
        <MetricCard
          label="Tasks Today"
          value={stats.tasks_today?.toLocaleString() ?? tasks.length.toLocaleString()}
          color="var(--blue)"
        />
        <MetricCard
          label="Haiku %"
          value={fmtPct(stats.haiku_pct)}
          sub="of all tasks"
          color="var(--amber)"
        />
        <MetricCard
          label="Cost Today"
          value={stats.cost_today !== undefined ? '$' + Number(stats.cost_today).toFixed(2) : '—'}
          color="var(--green)"
        />
        <MetricCard
          label="Takeovers"
          value={stats.takeovers?.toLocaleString() ?? '—'}
          sub="escalations today"
          color="var(--coral)"
        />
      </div>

      {loading && <div className="loading-state">Loading routing data...</div>}
      {error   && <div className="error-state"><span>Failed to load routing</span><span style={{ fontSize: 12 }}>{error}</span></div>}

      {!loading && !error && (
        <>
          {tasks.length === 0 ? (
            <div className="empty-state">No routing tasks found</div>
          ) : (
            <div className="data-list">
              {tasks.map((t, i) => (
                <TaskRow key={t.id ?? t.task_id ?? i} task={t} />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
