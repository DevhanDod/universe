function tokenCount(n) {
  if (n === undefined || n === null) return null;
  return `${n.toLocaleString()} tok`;
}

function savingsPct(before, after) {
  if (!before || !after || before === 0) return null;
  const pct = ((before - after) / before) * 100;
  return pct.toFixed(1) + '%';
}

export default function CompressionDiff({ sample }) {
  if (!sample) return null;

  const {
    before_text,
    after_text,
    before_tokens,
    after_tokens,
    graph_shorthand,
    input_before_tokens,
    input_after_tokens,
    output_before_tokens,
    output_after_tokens,
  } = sample;

  const bTok = before_tokens ?? input_before_tokens ?? output_before_tokens;
  const aTok = after_tokens ?? input_after_tokens ?? output_after_tokens;
  const pct = savingsPct(bTok, aTok);

  return (
    <div className="compression-diff">
      <div className="compression-diff-header">
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          Compression Sample
        </span>
        {pct && <span className="savings-badge">-{pct} saved</span>}
      </div>

      <div className="compression-diff-body">
        <div className="compression-side before">
          <div className="compression-side-label">
            <span>Before</span>
            {bTok !== undefined && bTok !== null && (
              <span className="mono" style={{ fontSize: 10 }}>{tokenCount(bTok)}</span>
            )}
          </div>
          <div className="compression-text">
            {before_text || <span style={{ fontStyle: 'italic' }}>—</span>}
          </div>
        </div>

        <div className="compression-side after">
          <div className="compression-side-label">
            <span>After</span>
            {aTok !== undefined && aTok !== null && (
              <span className="mono" style={{ fontSize: 10 }}>{tokenCount(aTok)}</span>
            )}
          </div>
          <div className="compression-text after">
            {after_text || <span style={{ fontStyle: 'italic' }}>—</span>}
          </div>
        </div>
      </div>

      {graph_shorthand && (
        <div className="compression-shorthand">
          <div className="compression-shorthand-label">Graph Shorthand</div>
          <pre className="code-block" style={{ maxHeight: 80, fontSize: 11 }}>
            {graph_shorthand}
          </pre>
        </div>
      )}
    </div>
  );
}
