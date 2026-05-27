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
      { value: 'pending',   label: 'Pending' },
      { value: 'executing', label: 'Executing' },
      { value: 'completed', label: 'Completed' },
      { value: 'verified',  label: 'Verified' },
      { value: 'failed',    label: 'Failed' },
      { value: 'rejected',  label: 'Rejected' },
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

function truncate(s, n = 60) {
  if (!s) return '—';
  return s.length > n ? s.slice(0, n) + '…' : s;
}

function PlanDetail({ planId }) {
  const [detail, setDetail] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!planId) return;
    setLoading(true);
    fetch(`/api/plans/${planId}`)
      .then(r => r.json())
      .then(d => { setDetail(d); setLoading(false); })
      .catch(() => setLoading(false));
  }, [planId]);

  if (loading) return <div style={{ color: 'var(--text-muted)', fontSize: 13, padding: 8 }}>Loading...</div>;
  if (!detail) return null;

  const steps = detail.steps || [];
  const filesToChange = detail.files_to_change || [];
  const filesChanged = detail.files_changed || [];

  return (
    <div style={{ padding: '12px 16px', borderTop: '1px solid var(--border)', background: 'rgba(0,0,0,0.15)' }}>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 24, marginBottom: 16 }}>
        {/* Steps */}
        <div>
          <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 8 }}>Steps</div>
          {steps.length === 0
            ? <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>No steps</div>
            : steps.map((step, i) => (
                <div key={i} style={{ fontSize: 12, color: 'var(--text)', marginBottom: 4, display: 'flex', gap: 8 }}>
                  <span style={{ color: 'var(--text-muted)', flexShrink: 0 }}>{i + 1}.</span>
                  <span>{step}</span>
                </div>
              ))
          }
        </div>

        {/* Files */}
        <div>
          {filesToChange.length > 0 && (
            <>
              <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 8 }}>Files to Change</div>
              {filesToChange.map((f, i) => (
                <div key={i} style={{ fontSize: 11, color: 'var(--text-muted)', fontFamily: 'JetBrains Mono, monospace', marginBottom: 2 }}>{f}</div>
              ))}
            </>
          )}
          {filesChanged.length > 0 && (
            <>
              <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--green)', textTransform: 'uppercase', letterSpacing: '0.06em', margin: '12px 0 8px' }}>Actually Changed</div>
              {filesChanged.map((f, i) => (
                <div key={i} style={{ fontSize: 11, color: 'var(--text)', fontFamily: 'JetBrains Mono, monospace', marginBottom: 2 }}>{f}</div>
              ))}
            </>
          )}
        </div>
      </div>

      {/* Timeline */}
      <div style={{ marginBottom: 16 }}>
        <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 10 }}>Lifecycle</div>
        <PlanTimeline plan={detail} />
      </div>

      {/* Result + verification */}
      {detail.result_summary && (
        <div style={{ marginBottom: 12 }}>
          <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 6 }}>Result</div>
          <div style={{ fontSize: 12, color: 'var(--text)', lineHeight: 1.5 }}>{detail.result_summary}</div>
        </div>
      )}

      {detail.verification_note && (
        <div style={{ marginBottom: 12 }}>
          <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 6 }}>Verification Note</div>
          <div style={{ fontSize: 12, color: 'var(--text)', lineHeight: 1.5 }}>{detail.verification_note}</div>
        </div>
      )}

      {/* Cost */}
      {(detail.estimated_cost_usd !== undefined || detail.actual_cost_usd !== undefined) && (
        <div style={{ display: 'flex', gap: 24 }}>
          {detail.estimated_cost_usd !== undefined && (
            <div>
              <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Estimated: </span>
              <span style={{ fontSize: 11, fontFamily: 'JetBrains Mono, monospace', color: 'var(--text)' }}>${Number(detail.estimated_cost_usd).toFixed(4)}</span>
            </div>
          )}
          {detail.actual_cost_usd !== undefined && (
            <div>
              <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Actual: </span>
              <span style={{ fontSize: 11, fontFamily: 'JetBrains Mono, monospace', color: 'var(--text)' }}>${Number(detail.actual_cost_usd).toFixed(4)}</span>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function PlanRow({ plan }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div style={{ borderBottom: '1px solid var(--border)' }}>
      <div
        onClick={() => setExpanded(e => !e)}
        style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '10px 16px', cursor: 'pointer' }}
        className="task-collapsed"
      >
        <StatusBadge status={plan.status} />
        <span style={{ flex: 1, fontSize: 13, color: 'var(--text)' }}>{truncate(plan.title)}</span>
        {plan.step_count !== undefined && (
          <span style={{ fontSize: 11, color: 'var(--text-muted)', flexShrink: 0 }}>{plan.step_count} steps</span>
        )}
        {plan.skill_used && plan.skill_verified && (
          <span style={{
            fontSize: 11, padding: '1px 6px', borderRadius: 3,
            background: 'rgba(52,199,89,0.12)', color: 'var(--green)',
          }}>skill</span>
        )}
        {plan.planner_model && (
          <span style={{ fontSize: 11, color: 'var(--text-muted)', flexShrink: 0, fontFamily: 'JetBrains Mono, monospace' }}>
            {plan.planner_model}
          </span>
        )}
        <span style={{ fontSize: 11, color: 'var(--text-muted)', flexShrink: 0 }}>{fmtRelative(plan.created_at)}</span>
        <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{expanded ? '▲' : '▼'}</span>
      </div>
      {expanded && <PlanDetail planId={plan.id} />}
    </div>
  );
}

export default function Plans() {
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
  const total = stats?.total_plans ?? plans.length;
  const completionRate = stats?.completion_rate !== undefined
    ? (Number(stats.completion_rate) * 100).toFixed(0) + '%'
    : '—';
  const avgSteps = stats?.avg_steps !== undefined
    ? Number(stats.avg_steps).toFixed(1)
    : '—';

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Plans</div>
        <div className="page-subtitle">Plan-bridge lifecycle — planner → executor → verification</div>
      </div>

      <Filters filters={filters} onChange={f => setFilters(f)} fields={FILTER_FIELDS} />

      <div className="metric-grid" style={{ marginBottom: 24 }}>
        <MetricCard label="Total Plans" value={total.toLocaleString()} color="var(--blue)" />
        <MetricCard label="Completion Rate" value={completionRate} sub="completed + verified" color="var(--green)" />
        <MetricCard label="Avg Steps" value={avgSteps} sub="per plan" color="var(--amber)" />
      </div>

      {loading && <div className="loading-state">Loading plans...</div>}
      {error   && <div className="error-state"><span>Failed to load plans</span><span style={{ fontSize: 12 }}>{error}</span></div>}

      {!loading && !error && (
        plans.length === 0
          ? <div className="empty-state">No plans yet. Plans are created by the planner agent via the MCP store_plan tool.</div>
          : <div className="data-list" style={{ padding: 0 }}>
              {plans.map((p, i) => <PlanRow key={p.id ?? i} plan={p} />)}
            </div>
      )}
    </div>
  );
}
