function dotClass(status) {
  if (!status) return 'unknown';
  const s = status.toLowerCase();
  if (s === 'active')   return 'active';
  if (s === 'degraded') return 'degraded';
  if (s === 'error')    return 'error';
  return 'unknown';
}

export default function EngineStatus({ engine }) {
  if (!engine) return null;
  const { number, name, status, detail } = engine;

  return (
    <div className="engine-status-card">
      <div className="engine-number-badge">{number ?? '?'}</div>
      <div className="engine-info">
        <div className="engine-name">{name || `Engine ${number}`}</div>
        {detail && <div className="engine-detail">{detail}</div>}
      </div>
      <div className={`engine-status-dot ${dotClass(status)}`} title={status} />
    </div>
  );
}
