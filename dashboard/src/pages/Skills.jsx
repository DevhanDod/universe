import { useState, useEffect, useCallback } from 'react';
import Filters from '../components/Filters.jsx';
import MetricCard from '../components/MetricCard.jsx';
import Badge from '../components/Badge.jsx';
import ConfidenceBar from '../components/ConfidenceBar.jsx';
import SkillTree from '../components/SkillTree.jsx';
import GraphNodeLink from '../components/GraphNodeLink.jsx';

const FILTER_FIELDS = [
  {
    name: 'language',
    label: 'Language',
    type: 'select',
    options: [
      { value: 'javascript', label: 'JavaScript' },
      { value: 'python',     label: 'Python' },
      { value: 'typescript', label: 'TypeScript' },
      { value: 'java',       label: 'Java' },
      { value: 'csharp',     label: 'C#' },
      { value: 'go',         label: 'Go' },
      { value: 'rust',       label: 'Rust' },
    ],
  },
  {
    name: 'sort',
    label: 'Sort',
    type: 'select',
    options: [
      { value: 'success_rate_desc', label: 'Success Rate ↓' },
      { value: 'applied_desc',      label: 'Most Applied' },
      { value: 'confidence_desc',   label: 'Confidence ↓' },
      { value: 'name_asc',          label: 'Name A-Z' },
    ],
  },
];

function SkillRow({ skill }) {
  const [expanded, setExpanded] = useState(false);

  const {
    name,
    version,
    evolution,
    language,
    success_rate,
    confidence,
    applied_count,
    instruction,
    lineage,
    derived,
    graph_nodes,
    negative_tags,
  } = skill;

  return (
    <div className="skill-row">
      <div className="skill-collapsed" onClick={() => setExpanded(e => !e)}>
        <div className="skill-name">{name || '—'}</div>
        <div className="skill-meta">
          {version && (
            <span className="mono" style={{ fontSize: 11, color: 'var(--blue)' }}>v{version}</span>
          )}
          {evolution && <Badge type={evolution} />}
          {language && <span className="tag language">{language}</span>}
          {applied_count !== undefined && (
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{applied_count}x applied</span>
          )}
          {success_rate !== undefined && success_rate !== null && (
            <div style={{ width: 80 }}>
              <ConfidenceBar value={success_rate} />
            </div>
          )}
          {confidence !== undefined && confidence !== null && success_rate === undefined && (
            <div style={{ width: 80 }}>
              <ConfidenceBar value={confidence} />
            </div>
          )}
        </div>
        <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{expanded ? '▲' : '▼'}</span>
      </div>

      {expanded && (
        <div className="skill-expanded">
          {instruction && (
            <div style={{ marginBottom: 14 }}>
              <div className="section-title" style={{ marginTop: 0 }}>Instruction</div>
              <pre className="code-block">{instruction}</pre>
            </div>
          )}

          {lineage && lineage.length > 0 && (
            <div style={{ marginBottom: 14 }}>
              <div className="section-title" style={{ marginTop: 0 }}>Version History</div>
              <SkillTree lineage={lineage} derived={derived} />
            </div>
          )}

          {graph_nodes && graph_nodes.length > 0 && (
            <div style={{ marginBottom: 12 }}>
              <div className="section-title" style={{ marginTop: 0 }}>Graph Nodes</div>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                {graph_nodes.map((n, i) => (
                  <GraphNodeLink key={i} nodeId={n} />
                ))}
              </div>
            </div>
          )}

          {negative_tags && negative_tags.length > 0 && (
            <div>
              <div className="section-title" style={{ marginTop: 0 }}>Negative Tags</div>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                {negative_tags.map((t, i) => (
                  <span key={i} className="tag negative">{t}</span>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export default function Skills() {
  const [filters, setFilters] = useState({});
  const [data, setData]       = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError]     = useState(null);

  const fetchData = useCallback(() => {
    setLoading(true);
    setError(null);
    const params = new URLSearchParams();
    Object.entries(filters).forEach(([k, v]) => { if (v) params.set(k, v); });
    fetch(`/api/skills?${params.toString()}`)
      .then(r => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then(d => { setData(d); setLoading(false); })
      .catch(e => { setError(e.message); setLoading(false); });
  }, [filters]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const skills       = data?.skills || data?.items || data?.results || [];
  const frozen       = skills.filter(s => s.frozen || s.status === 'frozen');
  const active       = skills.filter(s => !s.frozen && s.status !== 'frozen');
  const stats        = data?.stats || {};

  const activeCount  = stats.active_count  ?? active.length;
  const frozenCount  = stats.frozen_count  ?? frozen.length;
  const avgSuccess   = stats.avg_success_rate;

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Skills</div>
        <div className="page-subtitle">Learned patterns and evolved instructions</div>
      </div>

      <Filters filters={filters} onChange={f => { setFilters(f); }} fields={FILTER_FIELDS} />

      <div className="metric-grid" style={{ marginBottom: 24 }}>
        <MetricCard
          label="Active Skills"
          value={activeCount?.toLocaleString() ?? '—'}
          color="var(--green)"
        />
        <MetricCard
          label="Avg Success Rate"
          value={avgSuccess !== undefined ? (avgSuccess * 100).toFixed(1) + '%' : '—'}
          color="var(--blue)"
        />
        <MetricCard
          label="Frozen"
          value={frozenCount?.toLocaleString() ?? '—'}
          sub="awaiting review"
          color="var(--amber)"
        />
      </div>

      {loading && <div className="loading-state">Loading skills...</div>}
      {error   && <div className="error-state"><span>Failed to load skills</span><span style={{ fontSize: 12 }}>{error}</span></div>}

      {!loading && !error && (
        <>
          {frozen.length > 0 && (
            <div className="frozen-section" style={{ marginBottom: 24 }}>
              <div className="frozen-section-header">
                <span>⚠</span> Frozen Skills ({frozen.length})
              </div>
              <div>
                {frozen.map((s, i) => (
                  <SkillRow key={s.id ?? s.name ?? i} skill={s} />
                ))}
              </div>
            </div>
          )}

          {active.length === 0 && frozen.length === 0 ? (
            <div className="empty-state">No skills found</div>
          ) : (
            <div className="data-list">
              {active.map((s, i) => (
                <SkillRow key={s.id ?? s.name ?? i} skill={s} />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
