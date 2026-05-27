import { NavLink, Outlet } from 'react-router-dom';
import { useState, useEffect } from 'react';

const NAV_LINKS = [
  { to: '/',            label: 'Overview',    icon: '◈' },
  { to: '/graph',       label: 'Graph',       icon: '⬡' },
  { to: '/memory',      label: 'Your Memory', icon: '◎' },
  { to: '/skills',      label: 'Skills',      icon: '◆' },
  { to: '/plans',       label: 'Plans',       icon: '▤' },
  { to: '/compression', label: 'Compression', icon: '⊞' },
  { to: '/activity',    label: 'Activity',    icon: '⇢' },
];

function statusClass(status) {
  if (!status) return '';
  const s = status.toLowerCase();
  if (s === 'active')    return 'active';
  if (s === 'degraded')  return 'degraded';
  if (s === 'error')     return 'error';
  return '';
}

export default function Layout() {
  const [engines, setEngines] = useState([]);

  useEffect(() => {
    fetch('/api/overview')
      .then(r => r.json())
      .then(data => {
        if (data && data.engines) setEngines(data.engines);
      })
      .catch(() => {});
  }, []);

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="sidebar-logo">
          <h1>Universe</h1>
          <span>AI Platform</span>
        </div>

        <nav className="sidebar-nav">
          {NAV_LINKS.map(link => (
            <NavLink
              key={link.to}
              to={link.to}
              end={link.to === '/'}
              className={({ isActive }) => isActive ? 'active' : ''}
            >
              <span className="nav-icon">{link.icon}</span>
              {link.label}
            </NavLink>
          ))}
        </nav>

        <div className="sidebar-footer">
          <div className="sidebar-footer-label">Engines</div>
          <div className="engine-dots">
            {engines.length === 0
              ? [1,2,3,4,5].map(n => (
                  <div key={n} className="engine-dot" title={`Engine ${n}`} />
                ))
              : engines.map(eng => (
                  <div
                    key={eng.number ?? eng.id ?? eng.name}
                    className={`engine-dot ${statusClass(eng.status)}`}
                    title={`${eng.name || 'Engine ' + eng.number}: ${eng.status}`}
                  />
                ))
            }
          </div>
        </div>
      </aside>

      <main className="main-content">
        <Outlet />
      </main>
    </div>
  );
}
