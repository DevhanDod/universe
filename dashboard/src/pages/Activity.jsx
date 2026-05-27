import { useState, useEffect, useCallback } from 'react';
import MetricCard from '../components/MetricCard.jsx';
import StatusBadge from '../components/StatusBadge.jsx';
import PlanTimeline from '../components/PlanTimeline.jsx';
import Filters from '../components/Filters.jsx';

const FILTER_FIELDS = [
  { name: 'developer', label: 'Developer', type: 'text', placeholder: 'Filter by dev...' },
  {
    name: 'status',
    label: 'Status',
    type: 'pills',
    options: [
      { value: 'verified',  label: 'Verified' },
      { value: 'rejected',  label: 'Rejected' },
      { value: 'failed',    label: 'Failed' },
      { value: 'completed', label: 'Completed' },
    ],
  },
];

function fmtRelative(ts) {
  if (!ts) return '—';
  try {
    const diff = Date.now() - new Date(ts).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 1)  return 'just now';
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24)  return `${hrs}h ago`;
    return `${Math.floor(hrs / 24)}d ago`;
  } catch {
    return ts;
  }
}

function fmtCurrency(n) {
  if (n === undefined || n === null) return '—';
  return '$' + Number(n).toFixed(4);
}

function ActivityRow({ plan }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div style={{ borderBottom: '1px solid var(--border)' }}>
      <div
        onClick={() => setExpanded(e => !e)}
        style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '10px 16px', cursor: 'pointer' }}
        className="task-collapsed"
      >
        <StatusBadge status={plan.status} />
        <span style={{ flex: 1, fontSize: 13, color: 'var(--text)' }}>
          {plan.title || '—'}
        </span>
        {plan.skill_used && (
          <span style={{
            fontSize: 11, padding: '1px 6px', borderRadius: 3,
            background: 'rgba(43,124,201,0.12)', color: 'var(--blue)',
          }}>
            {plan.skill_used}
          </span>
        )}
        {plan.estimated_cost_usd !== undefined && (
          <span style={{ fontSize: 11, color: 'var(--text-muted)', flexShrink: 0, fontFamily: 'JetBrains Mono, monospace' }}>
            {fmtCurrency(plan.estimated_cost_usd)}
          </span>
        )}
        <span style={{ fontSize: 11, color: 'var(--text-muted)', flexShrink: 0 }}>{fmtRelative(plan.created_at)}</span>
        <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{expanded ? '▲' : '▼'}</span>
      </div>

      {expanded && (
        <div style={{ padding: '12px 16px', borderTop: '1px solid var(--border)', background: 'rgba(0,0,0,0.15)' }}>
          <div style={{ marginBottom: 16 }}>
            <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 10 }}>Lifecycle</div>
            <PlanTimeline plan={plan} />
          </div>

          {plan.result_summary && (
            <div style={{ marginBottom: 12 }}>
              <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 6 }}>Result</div>
              <div style={{ fontSize: 12, color: 'var(--text)', lineHeight: 1.5 }}>{plan.result_summary}</div>
            </div>
          )}

          {plan.verification_note && (
            <div style={{ marginBottom: 12 }}>
              <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 6 }}>Verification</div>
              <div style={{ fontSize: 12, color: 'var(--text)', lineHeight: 1.5 }}>{plan.verification_note}</div>
            </div>
          )}

          <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap' }}>
            {plan.planner_model && (
              <div>
                <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Planner: </span>
                <span style={{ fontSize: 11, fontFamily: 'JetBrains Mono, monospace', color: 'var(--text)' }}>{plan.planner_model}</span>
              </div>
            )}
            {plan.executor_model && (
              <div>
                <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Executor: </span>
                <span style={{ fontSize: 11, fontFamily: 'JetBrains Mono, monospace', color: 'var(--text)' }}>{plan.executor_model}</span>
              </div>
            )}
            {plan.estimated_cost_usd !== undefined && (
              <div>
                <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Est. cost: </span>
                <span style={{ fontSize: 11, fontFamily: 'JetBrains Mono, monospace', color: 'var(--text)' }}>{fmtCurrency(plan.estimated_cost_usd)}</span>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

export default function Activity() {
  const [filters, setFilters] = useState({});
  const [data, setData]       = useState(null);
  const [stats, setStats]     = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError]     = useState(null);

  const fetchData = useCallback(() => {
    setLoading(true);
    setError(null);
    const params = new URLSearchParams();
    Object.entries(filters).forEach(([k, v]) => { if (v) params.set(k, v); });

    Promise.all([
      fetch(`/api/plans?${params.toString()}`).then(r => r.json()),
      fetch('/api/plans/stats').then(r => r.json()),
    ])
      .then(([plansData, statsData]) => {
        setData(plansData);
        setStats(statsData);
        setLoading(false);
      })
      .catch(e => { setError(e.message); setLoading(false); });
  }, [filters]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const plans = data?.plans || data?.items || [];

  const plansToday   = stats?.plans_today ?? '—';
  const verifiedPct  = stats?.verification_rate !== undefined
    ? (Number(stats.verification_rate) * 100).toFixed(0) + '%'
    : '—';
  const estSavings   = stats?.estimated_savings_usd !== undefined
    ? '$' + Number(stats.estimated_savings_usd).toFixed(2)
    : '—';
  const failedToday  = stats?.failed_today ?? '—';

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Activity</div>
        <div className="page-subtitle">Plan lifecycle — creation, execution, and verification events</div>
      </div>

      <Filters filters={filters} onChange={f => setFilters(f)} fields={FILTER_FIELDS} />

      <div className="metric-grid" style={{ marginBottom: 24 }}>
        <MetricCard label="Plans Today"    value={String(plansToday)} color="var(--blue)" />
        <MetricCard label="Verified"       value={verifiedPct} sub="of completed" color="var(--green)" />
        <MetricCard label="Est. Savings"   value={estSavings}  sub="vs. all-premium" color="var(--green)" trend="up" />
        <MetricCard label="Failed"         value={String(failedToday)} sub="today" color="var(--coral)" />
      </div>

      {loading && <div className="loading-state">Loading activity...</div>}
      {error   && <div className="error-state"><span>Failed to load activity</span><span style={{ fontSize: 12 }}>{error}</span></div>}

      {!loading && !error && (
        plans.length === 0
          ? <div className="empty-state">No activity yet. Plan lifecycle events appear here as the planner and executor agents work.</div>
          : <div className="data-list" style={{ padding: 0 }}>
              {plans.map((p, i) => <ActivityRow key={p.id ?? i} plan={p} />)}
            </div>
      )}
    </div>
  );
}
