import Badge from './Badge.jsx';

function stepDotClass(action) {
  if (!action) return 'normal';
  const a = action.toLowerCase();
  if (a.includes('escalat')) return 'escalation';
  if (a.includes('takeover')) return 'takeover';
  return 'normal';
}

function formatDuration(ms) {
  if (ms === undefined || ms === null) return null;
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export default function RoutingTrace({ trace = [] }) {
  if (!trace || trace.length === 0) {
    return <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>No trace data</div>;
  }

  return (
    <div className="routing-trace">
      {trace.map((step, i) => {
        const dotCls = stepDotClass(step.action);
        const duration = formatDuration(step.duration_ms);

        return (
          <div key={i} className="trace-step">
            <div className={`trace-dot ${dotCls}`}>{step.step ?? i + 1}</div>
            <div className="trace-content">
              <div className="trace-action">{step.action || '—'}</div>
              {step.detail && <div className="trace-detail">{step.detail}</div>}
              <div className="trace-meta">
                {step.model && <Badge type={step.model} label={step.model} />}
                {step.tokens !== undefined && step.tokens !== null && (
                  <span className="mono" style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                    {typeof step.tokens === 'object'
                      ? `${step.tokens.input ?? 0}↑ ${step.tokens.output ?? 0}↓`
                      : `${step.tokens} tok`}
                  </span>
                )}
                {duration && (
                  <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{duration}</span>
                )}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}
