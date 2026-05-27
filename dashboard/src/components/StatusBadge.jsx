const STATUS_STYLES = {
  pending:   { bg: 'rgba(120,120,140,0.15)', color: 'var(--text-muted)',  label: 'Pending' },
  executing: { bg: 'rgba(43,124,201,0.15)',  color: 'var(--blue)',        label: 'Executing' },
  completed: { bg: 'rgba(52,199,89,0.15)',   color: 'var(--green)',       label: 'Completed' },
  failed:    { bg: 'rgba(255,99,99,0.15)',   color: 'var(--coral)',       label: 'Failed' },
  verified:  { bg: 'rgba(52,199,89,0.2)',    color: 'var(--green)',       label: '✓ Verified' },
  rejected:  { bg: 'rgba(255,99,99,0.2)',    color: 'var(--coral)',       label: '✗ Rejected' },
};

export default function StatusBadge({ status }) {
  const s = (status || 'pending').toLowerCase();
  const style = STATUS_STYLES[s] || STATUS_STYLES.pending;
  return (
    <span style={{
      display: 'inline-block',
      padding: '2px 8px',
      borderRadius: 4,
      fontSize: 11,
      fontWeight: 600,
      background: style.bg,
      color: style.color,
      letterSpacing: '0.02em',
    }}>
      {style.label}
    </span>
  );
}
