import Badge from './Badge.jsx';
import GraphNodeLink from './GraphNodeLink.jsx';
import ConfidenceBar from './ConfidenceBar.jsx';

function formatTime(ts) {
  if (!ts) return '—';
  try {
    const d = new Date(ts);
    return d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  } catch {
    return ts;
  }
}

function developerInitial(dev) {
  if (!dev) return '?';
  return dev.charAt(0).toUpperCase();
}

export default function ObservationRow({ obs, expanded, onToggle }) {
  if (!obs) return null;

  const {
    timestamp,
    developer,
    category,
    summary,
    graph_node,
    confidence,
    detail,
    tool_calls,
  } = obs;

  return (
    <div className="obs-row">
      <div className="obs-collapsed" onClick={onToggle}>
        <span className="obs-time">{formatTime(timestamp)}</span>
        <div className="obs-avatar">{developerInitial(developer)}</div>
        <Badge type={category} />
        <span className="obs-summary">{summary || detail || '—'}</span>
        {graph_node && <GraphNodeLink nodeId={graph_node} />}
        {confidence !== undefined && confidence !== null && (
          <div style={{ width: 60, flexShrink: 0 }}>
            <ConfidenceBar value={confidence} />
          </div>
        )}
        <span style={{ color: 'var(--text-muted)', fontSize: 11, marginLeft: 'auto' }}>
          {expanded ? '▲' : '▼'}
        </span>
      </div>

      {expanded && (
        <div className="obs-expanded">
          {detail && <pre className="obs-detail-block">{detail}</pre>}
          {tool_calls && tool_calls.length > 0 && (
            <div className="obs-tool-calls">
              <h4>Tool Calls</h4>
              {tool_calls.map((tc, i) => (
                <div key={i} className="obs-tool-call-item">
                  {typeof tc === 'string' ? tc : JSON.stringify(tc)}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
