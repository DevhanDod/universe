export default function Filters({ filters = {}, onChange, fields = [] }) {
  function handleChange(name, value) {
    onChange({ ...filters, [name]: value });
  }

  return (
    <div className="filter-bar">
      {fields.map(field => {
        if (field.type === 'select') {
          return (
            <label key={field.name}>
              {field.label}
              <select
                value={filters[field.name] || ''}
                onChange={e => handleChange(field.name, e.target.value)}
              >
                <option value="">All</option>
                {(field.options || []).map(opt => (
                  <option key={opt.value ?? opt} value={opt.value ?? opt}>
                    {opt.label ?? opt}
                  </option>
                ))}
              </select>
            </label>
          );
        }

        if (field.type === 'text') {
          return (
            <label key={field.name}>
              {field.label}
              <input
                type="text"
                placeholder={field.placeholder || field.label}
                value={filters[field.name] || ''}
                onChange={e => handleChange(field.name, e.target.value)}
              />
            </label>
          );
        }

        if (field.type === 'date') {
          return (
            <label key={field.name}>
              {field.label}
              <input
                type="date"
                value={filters[field.name] || ''}
                onChange={e => handleChange(field.name, e.target.value)}
                style={{ colorScheme: 'dark' }}
              />
            </label>
          );
        }

        if (field.type === 'pills') {
          const active = filters[field.name] || '';
          return (
            <div key={field.name} style={{ display: 'flex', gap: '6px', alignItems: 'center', flexWrap: 'wrap' }}>
              <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>{field.label}:</span>
              <button
                className={`filter-pill ${active === '' ? 'active' : ''}`}
                onClick={() => handleChange(field.name, '')}
              >All</button>
              {(field.options || []).map(opt => {
                const val = opt.value ?? opt;
                return (
                  <button
                    key={val}
                    className={`filter-pill ${active === val ? 'active' : ''}`}
                    onClick={() => handleChange(field.name, val)}
                  >
                    {opt.label ?? opt}
                  </button>
                );
              })}
            </div>
          );
        }

        return null;
      })}
    </div>
  );
}
