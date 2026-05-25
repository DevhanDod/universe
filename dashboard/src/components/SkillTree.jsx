import Badge from './Badge.jsx';

function SkillNode({ version, evolution, creator }) {
  return (
    <div className="skill-tree-box">
      <div className="skill-tree-version">v{version}</div>
      {evolution && <div style={{ marginBottom: 3 }}><Badge type={evolution} /></div>}
      {creator && <div className="skill-tree-creator">{creator}</div>}
    </div>
  );
}

export default function SkillTree({ lineage = [], derived = [] }) {
  if (!lineage || lineage.length === 0) {
    return <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>No version history</div>;
  }

  return (
    <div style={{ overflowX: 'auto', paddingBottom: 8 }}>
      <div className="skill-tree">
        {lineage.map((ver, i) => (
          <div key={ver.version ?? i} className="skill-tree-node">
            <SkillNode
              version={ver.version ?? i + 1}
              evolution={ver.evolution}
              creator={ver.creator}
            />
            {i < lineage.length - 1 && (
              <span className="skill-tree-arrow">→</span>
            )}
          </div>
        ))}
      </div>

      {derived && derived.length > 0 && (
        <div style={{ marginTop: 12 }}>
          <div style={{ fontSize: 11, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.07em', marginBottom: 8 }}>
            Derived Skills
          </div>
          <div className="skill-tree-branch">
            {derived.map((d, i) => (
              <div key={d.version ?? i} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>↳</span>
                <SkillNode
                  version={d.version ?? i + 1}
                  evolution={d.evolution}
                  creator={d.creator}
                />
                {d.name && (
                  <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{d.name}</span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
