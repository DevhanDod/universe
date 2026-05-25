export default function ConfidenceBar({ value = 0, color }) {
  const pct = Math.max(0, Math.min(1, value)) * 100;

  let fillColor = color;
  if (!fillColor) {
    if (value < 0.3)      fillColor = 'var(--coral)';
    else if (value < 0.6) fillColor = 'var(--amber)';
    else                  fillColor = 'var(--green)';
  }

  return (
    <div className="confidence-bar-wrap" style={{ width: '100%' }}>
      <div
        className="confidence-bar-fill"
        style={{ width: `${pct}%`, background: fillColor }}
      />
    </div>
  );
}
