function fmtTime(ts) {
  if (!ts) return '';
  try {
    return new Date(ts).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
  } catch {
    return ts;
  }
}

const STEP_ICONS = {
  created:   '📋',
  executing: '⚡',
  completed: '✅',
  failed:    '❌',
  verified:  '🧠',
  rejected:  '🚫',
};

export default function PlanTimeline({ plan }) {
  if (!plan) return null;

  const events = [];

  if (plan.created_at) {
    events.push({
      icon: STEP_ICONS.created,
      label: 'Plan created',
      detail: plan.planner_model || '',
      ts: plan.created_at,
    });
  }

  if (plan.started_at || plan.status === 'executing') {
    events.push({
      icon: STEP_ICONS.executing,
      label: 'Executor picked up plan',
      detail: plan.executor_model || '',
      ts: plan.started_at,
    });
  }

  if (plan.executed_at && (plan.status === 'completed' || plan.status === 'failed' || plan.status === 'verified' || plan.status === 'rejected')) {
    const filesChanged = plan.files_changed?.length
      ? `${plan.files_changed.length} file${plan.files_changed.length !== 1 ? 's' : ''} changed`
      : '';
    events.push({
      icon: plan.result_success === false ? STEP_ICONS.failed : STEP_ICONS.completed,
      label: plan.result_success === false ? 'Execution failed' : `Execution completed${filesChanged ? ' — ' + filesChanged : ''}`,
      detail: plan.executor_model || '',
      ts: plan.executed_at,
    });
  }

  if (plan.verified_at) {
    const isRejected = plan.status === 'rejected';
    events.push({
      icon: isRejected ? STEP_ICONS.rejected : STEP_ICONS.verified,
      label: isRejected ? 'Planner rejected' : 'Planner verified — approved',
      detail: plan.planner_model || plan.verification_note || '',
      ts: plan.verified_at,
    });
  }

  return (
    <div style={{ padding: '8px 0' }}>
      {events.map((ev, i) => (
        <div key={i} style={{ display: 'flex', gap: 12, marginBottom: i < events.length - 1 ? 16 : 0, position: 'relative' }}>
          {/* connector line */}
          {i < events.length - 1 && (
            <div style={{
              position: 'absolute',
              left: 10,
              top: 24,
              width: 1,
              height: 'calc(100% + 8px)',
              background: 'var(--border)',
            }} />
          )}
          <span style={{ fontSize: 16, flexShrink: 0, width: 20, textAlign: 'center' }}>{ev.icon}</span>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 13, color: 'var(--text)', fontWeight: 500 }}>{ev.label}</div>
            {ev.detail && (
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>{ev.detail}</div>
            )}
          </div>
          {ev.ts && (
            <span style={{ fontSize: 11, color: 'var(--text-muted)', flexShrink: 0, fontFamily: 'JetBrains Mono, monospace' }}>
              {fmtTime(ev.ts)}
            </span>
          )}
        </div>
      ))}
    </div>
  );
}
