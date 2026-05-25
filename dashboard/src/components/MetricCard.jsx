export default function MetricCard({ label, value, sub, color, trend }) {
  const valueColor = color || 'var(--text)';

  let trendEl = null;
  if (trend === 'up') {
    trendEl = <span className="metric-trend up">▲</span>;
  } else if (trend === 'down') {
    trendEl = <span className="metric-trend down">▼</span>;
  }

  return (
    <div className="metric-card">
      <div className="metric-label">{label}</div>
      <div className="metric-value" style={{ color: valueColor }}>
        {value}
        {trendEl}
      </div>
      {sub && <div className="metric-sub">{sub}</div>}
    </div>
  );
}
