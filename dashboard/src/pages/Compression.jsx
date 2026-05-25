import { useState, useEffect } from 'react';
import MetricCard from '../components/MetricCard.jsx';
import CompressionDiff from '../components/CompressionDiff.jsx';

function fmtPct(n) {
  if (n === undefined || n === null) return '—';
  if (typeof n === 'number') return n.toFixed(1) + '%';
  return n;
}

function fmtNum(n) {
  if (n === undefined || n === null) return '—';
  if (typeof n === 'number') return n.toLocaleString();
  return n;
}

export default function Compression() {
  const [data, setData]       = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError]     = useState(null);

  useEffect(() => {
    fetch('/api/compression/samples')
      .then(r => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then(d => { setData(d); setLoading(false); })
      .catch(e => { setError(e.message); setLoading(false); });
  }, []);

  const samples = data?.samples || data?.items || data?.results || [];
  const stats   = data?.stats || {};

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Compression</div>
        <div className="page-subtitle">Context compression samples and savings</div>
      </div>

      <div className="metric-grid" style={{ marginBottom: 24 }}>
        <MetricCard
          label="Avg Output Reduction"
          value={fmtPct(stats.avg_output_reduction)}
          sub="context compression"
          color="var(--green)"
        />
        <MetricCard
          label="Avg Input Reduction"
          value={fmtPct(stats.avg_input_reduction)}
          sub="prompt compression"
          color="var(--teal)"
        />
        <MetricCard
          label="Tokens Saved Today"
          value={fmtNum(stats.tokens_saved_today)}
          sub="across all sessions"
          color="var(--blue)"
        />
      </div>

      {loading && <div className="loading-state">Loading samples...</div>}
      {error   && <div className="error-state"><span>Failed to load compression data</span><span style={{ fontSize: 12 }}>{error}</span></div>}

      {!loading && !error && (
        <>
          {samples.length === 0 ? (
            <div className="empty-state">No compression samples</div>
          ) : (
            <div>
              {samples.map((s, i) => (
                <CompressionDiff key={s.id ?? i} sample={s} />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
