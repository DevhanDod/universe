const TYPE_MAP = {
  fix:                'badge-coral',
  'category/fix':     'badge-coral',
  captured:           'badge-green',
  derived:            'badge-blue',
  'category/decision':'badge-blue',
  manual:             'badge-purple',
  premium:            'badge-purple',
  full_orchestration: 'badge-purple',
  single_opus:        'badge-purple',
  'category/pattern': 'badge-purple',
  skill_execute:      'badge-green',
  memory_apply:       'badge-teal',
  plan_execute:       'badge-blue',
  low_cost:           'badge-amber',
  single_haiku:       'badge-amber',
  automated:          'badge-green',
  'category/failure': 'badge-coral',
};

const DEFAULT_LABELS = {
  fix:                'Fix',
  'category/fix':     'Fix',
  captured:           'Captured',
  derived:            'Derived',
  'category/decision':'Decision',
  manual:             'Manual',
  premium:            'Premium',
  full_orchestration: 'Full Orch.',
  single_opus:        'Opus',
  'category/pattern': 'Pattern',
  skill_execute:      'Skill Exec',
  memory_apply:       'Mem Apply',
  plan_execute:       'Plan Exec',
  low_cost:           'Low Cost',
  single_haiku:       'Haiku',
  automated:          'Automated',
  'category/failure': 'Failure',
};

export default function Badge({ type, label }) {
  const cls = TYPE_MAP[type] || 'badge-muted';
  const text = label || DEFAULT_LABELS[type] || type || '—';
  return <span className={`badge ${cls}`}>{text}</span>;
}
