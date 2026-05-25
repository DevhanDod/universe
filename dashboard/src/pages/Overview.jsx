import { useState, useEffect } from 'react';
import {
  AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  PieChart, Pie, Cell, Legend,
} from 'recharts';
import EngineStatus from '../components/EngineStatus.jsx';
import MetricCard from '../components/MetricCard.jsx';

const PIE_COLORS = [
  'var(--blue)',
  'var(--purple)',
  'var(--amber)',
  'var(--teal)',
  'var(--coral)',
  'var(--green)',
];

function fmt(n) {
  if (n === undefined || n === null) return '—';
  if (typeof n === 'number') return n.toLocaleString();
  return n;
}

function fmtCurrency(n) {
  if (n === undefined || n === null) return '—';
  if (typeof n !== 'number') return n;
  return '$' + n.toFixed(2);
}

function fmtPct(n) {
  if (n === undefined || n === null) return '—';
  if (typeof n !== 'number') return n;
  return n.toFixed(1) + '%';
}

const CustomTooltip = ({ active, payload, label }) => {
  if (!active || !payload || !payload.length) return null;
  return (
    <div style={{
      background: 'var(--card-bg)',
      border: '1px solid var(--border)',
      borderRadius: 6,
      padding: '8px 12px',
      fontSize: 12,
    }}>
      <div style={{ color: 'var(--text-muted)', marginBottom: 4 }}>{label}</div>
      {payload.map((p, i) => (
        <div key={i} style={{ color: p.color, marginBottom: 2 }}>
          {p.name}: ${typeof p.value === 'number' ? p.value.toFixed(2) : p.value}
        </div>
      ))}
    </div>
  );
};

export default function Overview() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    fetch('/api/overview')
      .then(r => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then(d => { setData(d); setLoading(false); })
      .catch(e => { setError(e.message); setLoading(false); });
  }, []);

  if (loading) return <div className="loading-state">Loading overview...</div>;
  if (error)   return <div className="error-state"><span>Failed to load overview</span><span style={{ fontSize: 12 }}>{error}</span></div>;

  const {
    engines = [],
    cost = {},
    chart_data = [],
    routing_breakdown = [],
  } = data || {};

  const routingPieData = routing_breakdown.map(r => ({
    name: r.mode || r.name,
    value: r.count ?? r.value ?? 0,
  }));

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Overview</div>
        <div className="page-subtitle">Platform health and cost summary</div>
      </div>

      {/* Engine Status Strip */}
      {engines.length > 0 && (
        <>
          <div className="section-title">Engine Status</div>
          <div className="engine-status-row">
            {engines.map(eng => (
              <EngineStatus key={eng.number ?? eng.id ?? eng.name} engine={eng} />
            ))}
          </div>
        </>
      )}

      {/* Metric Cards */}
      <div className="metric-grid">
        <MetricCard
          label="Monthly Cost"
          value={fmtCurrency(cost.monthly_actual)}
          sub={`Budget: ${fmtCurrency(cost.monthly_budget)}`}
          color="var(--blue)"
        />
        <MetricCard
          label="Total Saved"
          value={fmtCurrency(cost.total_saved)}
          sub="vs. unoptimized"
          color="var(--green)"
          trend="up"
        />
        <MetricCard
          label="Savings %"
          value={fmtPct(cost.savings_pct)}
          sub="month-to-date"
          color="var(--green)"
        />
        <MetricCard
          label="Per-Task Cost"
          value={fmtCurrency(cost.per_task)}
          sub="avg this month"
          color="var(--amber)"
        />
      </div>

      {/* Charts */}
      <div className="charts-grid">
        <div className="chart-card">
          <div className="chart-title">Cost Trend (6 months)</div>
          {chart_data.length > 0 ? (
            <ResponsiveContainer width="100%" height={220}>
              <AreaChart data={chart_data} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
                <defs>
                  <linearGradient id="colorActual" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%"  stopColor="var(--blue)"  stopOpacity={0.3} />
                    <stop offset="95%" stopColor="var(--blue)"  stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="colorWould" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%"  stopColor="var(--coral)" stopOpacity={0.2} />
                    <stop offset="95%" stopColor="var(--coral)" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" />
                <XAxis dataKey="month" tick={{ fill: 'var(--text-muted)', fontSize: 11 }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fill: 'var(--text-muted)', fontSize: 11 }} axisLine={false} tickLine={false} tickFormatter={v => '$' + v} />
                <Tooltip content={<CustomTooltip />} />
                <Area
                  type="monotone"
                  dataKey="would_have_cost"
                  name="Without Optimization"
                  stroke="var(--coral)"
                  fill="url(#colorWould)"
                  strokeWidth={1.5}
                  strokeDasharray="4 2"
                />
                <Area
                  type="monotone"
                  dataKey="actual_cost"
                  name="Actual"
                  stroke="var(--blue)"
                  fill="url(#colorActual)"
                  strokeWidth={2}
                />
              </AreaChart>
            </ResponsiveContainer>
          ) : (
            <div className="empty-state">No chart data</div>
          )}
        </div>

        <div className="chart-card">
          <div className="chart-title">Routing Breakdown</div>
          {routingPieData.length > 0 ? (
            <ResponsiveContainer width="100%" height={220}>
              <PieChart>
                <Pie
                  data={routingPieData}
                  cx="50%"
                  cy="45%"
                  innerRadius={55}
                  outerRadius={80}
                  paddingAngle={3}
                  dataKey="value"
                >
                  {routingPieData.map((_, index) => (
                    <Cell key={index} fill={PIE_COLORS[index % PIE_COLORS.length]} />
                  ))}
                </Pie>
                <Tooltip
                  contentStyle={{ background: 'var(--card-bg)', border: '1px solid var(--border)', borderRadius: 6, fontSize: 12 }}
                  itemStyle={{ color: 'var(--text)' }}
                />
                <Legend
                  iconSize={8}
                  iconType="circle"
                  formatter={v => <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{v}</span>}
                />
              </PieChart>
            </ResponsiveContainer>
          ) : (
            <div className="empty-state">No routing data</div>
          )}
        </div>
      </div>
    </div>
  );
}
