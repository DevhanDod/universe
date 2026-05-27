# Step 8 — Build the Dashboard

## Build Specification for Claude Code

**Purpose:** Set up the React dashboard project, build it, embed the output into the Go binary, and verify it serves correctly at localhost:3001.  
**Estimated effort:** 3-4 hours  
**Dependencies:** All 5 engines built. `dashboard.md` describes WHAT to build. This file describes HOW to build and wire it.  

---

## 1. What We're Doing

The dashboard has two parts:
1. **React frontend** — a single-page app with 6 views (overview, graph, memory, skills, compression, routing)
2. **Go backend** — serves the React app as static files + REST API endpoints that query PostgreSQL

After building, `universe dashboard` starts a web server. The React app is embedded inside the Go binary using `go:embed` — no separate files, no Node.js runtime needed in production. One binary serves everything.

---

## 2. Set Up the React Project

### 2.1 Create the dashboard directory

```bash
mkdir -p dashboard/src/pages dashboard/src/components
cd dashboard
```

### 2.2 Create `dashboard/package.json`

```json
{
  "name": "universe-dashboard",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "react": "^18.3.0",
    "react-dom": "^18.3.0",
    "react-router-dom": "^6.23.0",
    "recharts": "^2.12.0"
  },
  "devDependencies": {
    "@types/react": "^18.3.0",
    "@types/react-dom": "^18.3.0",
    "@vitejs/plugin-react": "^4.3.0",
    "vite": "^5.4.0"
  }
}
```

### 2.3 Create `dashboard/vite.config.js`

```javascript
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    // Output to the Go package's static directory
    outDir: '../internal/dashboard/static',
    emptyOutDir: true,
    // Single bundle — no code splitting (simpler for embedding)
    rollupOptions: {
      output: {
        manualChunks: undefined,
      },
    },
  },
  server: {
    port: 5173,
    // Proxy API calls to the Go backend during development
    proxy: {
      '/api': {
        target: 'http://localhost:3001',
        changeOrigin: true,
      },
    },
  },
})
```

### 2.4 Create `dashboard/index.html`

```html
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Universe Dashboard</title>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet" />
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.jsx"></script>
  </body>
</html>
```

### 2.5 Create `dashboard/src/main.jsx`

```jsx
import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
)
```

### 2.6 Create `dashboard/src/App.jsx`

```jsx
import { useState, useEffect } from 'react'
import { BrowserRouter, Routes, Route, NavLink } from 'react-router-dom'
import Overview from './pages/Overview'
import Memory from './pages/Memory'
import Skills from './pages/Skills'
import Compression from './pages/Compression'
import Routing from './pages/Routing'
import GraphView from './pages/GraphView'

// Shared styles
const styles = {
  app: {
    display: 'flex',
    minHeight: '100vh',
    background: '#060810',
    color: '#e5e7eb',
    fontFamily: "'Inter', -apple-system, sans-serif",
  },
  sidebar: {
    width: 200,
    background: '#0A0C10',
    borderRight: '1px solid #1a1f2e',
    padding: '20px 0',
    display: 'flex',
    flexDirection: 'column',
    position: 'fixed',
    top: 0,
    left: 0,
    bottom: 0,
  },
  logo: {
    padding: '0 20px 20px',
    borderBottom: '1px solid #1a1f2e',
    marginBottom: 8,
  },
  logoText: {
    fontSize: 16,
    fontWeight: 500,
    margin: 0,
    color: '#f9fafb',
  },
  logoSub: {
    fontSize: 11,
    color: '#6b7280',
    margin: '2px 0 0',
  },
  nav: {
    flex: 1,
    padding: '8px 0',
  },
  navLink: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '10px 20px',
    fontSize: 13,
    color: '#6b7280',
    textDecoration: 'none',
    transition: 'all 0.15s',
    borderLeft: '2px solid transparent',
  },
  navLinkActive: {
    color: '#e5e7eb',
    background: '#111318',
    borderLeftColor: '#5DCAA5',
  },
  main: {
    flex: 1,
    marginLeft: 200,
    padding: '24px 28px',
    minWidth: 0,
  },
}

const navItems = [
  { path: '/', label: 'Overview', icon: '📊' },
  { path: '/graph', label: 'Graph', icon: '🔗' },
  { path: '/memory', label: 'Memory', icon: '🧠' },
  { path: '/skills', label: 'Skills', icon: '🧬' },
  { path: '/compression', label: 'Compression', icon: '📐' },
  { path: '/routing', label: 'Routing', icon: '🔀' },
]

export default function App() {
  return (
    <BrowserRouter>
      <div style={styles.app}>
        {/* Sidebar */}
        <div style={styles.sidebar}>
          <div style={styles.logo}>
            <p style={styles.logoText}>🌌 Universe</p>
            <p style={styles.logoSub}>Dashboard</p>
          </div>
          <nav style={styles.nav}>
            {navItems.map(item => (
              <NavLink
                key={item.path}
                to={item.path}
                end={item.path === '/'}
                style={({ isActive }) => ({
                  ...styles.navLink,
                  ...(isActive ? styles.navLinkActive : {}),
                })}
              >
                <span>{item.icon}</span>
                <span>{item.label}</span>
              </NavLink>
            ))}
          </nav>
        </div>

        {/* Main content */}
        <main style={styles.main}>
          <Routes>
            <Route path="/" element={<Overview />} />
            <Route path="/graph" element={<GraphView />} />
            <Route path="/memory" element={<Memory />} />
            <Route path="/skills" element={<Skills />} />
            <Route path="/compression" element={<Compression />} />
            <Route path="/routing" element={<Routing />} />
          </Routes>
        </main>
      </div>
    </BrowserRouter>
  )
}
```

### 2.7 Create the 6 page components

Each page calls the Go API and renders the data. Create these files:

**`dashboard/src/pages/Overview.jsx`** — calls `GET /api/overview`, shows engine status strip, 4 metric cards, cost trend chart, routing pie chart.

**`dashboard/src/pages/Memory.jsx`** — calls `GET /api/memory?page=1&limit=20`, shows filter bar, observation list with category badges and graph node links.

**`dashboard/src/pages/Skills.jsx`** — calls `GET /api/skills`, shows skill list with evolution badges. Clicking a skill calls `GET /api/skills/:id/lineage` and renders the version tree.

**`dashboard/src/pages/Compression.jsx`** — calls `GET /api/compression/samples`, shows before/after side-by-side and the token waterfall.

**`dashboard/src/pages/Routing.jsx`** — calls `GET /api/routing?page=1&limit=20`, shows task list. Clicking a task calls `GET /api/routing/:taskId` and renders the routing trace.

**`dashboard/src/pages/GraphView.jsx`** — calls `GET /api/graph/nodes` and `GET /api/graph/edges`, renders the interactive graph (reuse your existing graph visualizer code or use a library like react-force-graph).

**Detailed implementation for each page is in `dashboard.md`** (sections 8.4 and 8.5). Claude Code should read `dashboard.md` for the exact data shapes, component structure, and API response formats. This file covers the build and wiring process.

### 2.8 Create shared components

```
dashboard/src/components/
├── MetricCard.jsx      — number card (label, value, sub-text, trend)
├── Filters.jsx         — filter bar (dropdowns, pills, date range)
├── Badge.jsx           — category/evolution/model badge
├── ConfidenceBar.jsx   — thin colored bar showing 0.0-1.0
├── SkillTree.jsx       — visual v1 → v2 → v3 evolution tree
├── RoutingTrace.jsx    — vertical timeline of routing steps
├── CompressionDiff.jsx — side-by-side before/after
└── GraphNodeLink.jsx   — clickable graph node reference
```

### 2.9 API helper

Create `dashboard/src/api.js`:

```javascript
const BASE_URL = '/api'

export async function fetchAPI(path, params = {}) {
  const url = new URL(BASE_URL + path, window.location.origin)
  Object.entries(params).forEach(([key, val]) => {
    if (val !== undefined && val !== null && val !== '') {
      url.searchParams.set(key, val)
    }
  })
  const res = await fetch(url.toString())
  if (!res.ok) throw new Error(`API error: ${res.status}`)
  return res.json()
}

// Page-specific API calls
export const api = {
  overview:      ()                      => fetchAPI('/overview'),
  memory:        (params)                => fetchAPI('/memory', params),
  memoryDetail:  (id)                    => fetchAPI(`/memory/${id}`),
  skills:        (params)                => fetchAPI('/skills', params),
  skillDetail:   (id)                    => fetchAPI(`/skills/${id}`),
  skillLineage:  (id)                    => fetchAPI(`/skills/${id}/lineage`),
  compression:   (params)                => fetchAPI('/compression/samples', params),
  routing:       (params)                => fetchAPI('/routing', params),
  routingDetail: (taskId)                => fetchAPI(`/routing/${taskId}`),
  graphNodes:    ()                      => fetchAPI('/graph/nodes'),
  graphEdges:    ()                      => fetchAPI('/graph/edges'),
  graphNode:     (id)                    => fetchAPI(`/graph/node/${id}`),
  costSummary:   (params)                => fetchAPI('/cost-summary', params),
}
```

---

## 3. Build the React App

```bash
cd dashboard

# Install dependencies
npm install

# Development mode (hot reload at localhost:5173, proxies API to localhost:3001)
npm run dev

# Production build (outputs to internal/dashboard/static/)
npm run build
```

After `npm run build`, verify the output:

```bash
ls -la ../internal/dashboard/static/
# Expected:
#   index.html
#   assets/
#     index-XXXXXX.js
#     index-XXXXXX.css
```

---

## 4. Go Backend: Embed and Serve

### 4.1 Ensure `internal/dashboard/server.go` uses `go:embed`

```go
package dashboard

import (
    "embed"
    "io/fs"
    "net/http"
)

//go:embed static/*
var staticFiles embed.FS

func (s *Server) registerRoutes() {
    // API routes
    s.mux.HandleFunc("/api/overview", s.HandleOverview)
    s.mux.HandleFunc("/api/memory", s.HandleMemoryList)
    s.mux.HandleFunc("/api/memory/", s.HandleMemoryDetail)
    s.mux.HandleFunc("/api/skills", s.HandleSkillsList)
    s.mux.HandleFunc("/api/skills/", s.HandleSkillDetail) // also handles /lineage
    s.mux.HandleFunc("/api/compression/samples", s.HandleCompressionSamples)
    s.mux.HandleFunc("/api/routing", s.HandleRoutingList)
    s.mux.HandleFunc("/api/routing/", s.HandleRoutingDetail)
    s.mux.HandleFunc("/api/graph/nodes", s.HandleGraphNodes)
    s.mux.HandleFunc("/api/graph/edges", s.HandleGraphEdges)
    s.mux.HandleFunc("/api/graph/node/", s.HandleGraphNodeDetail)

    // Static files — serve the React SPA
    // Strip the "static" prefix so index.html is at /
    staticFS, _ := fs.Sub(staticFiles, "static")
    fileServer := http.FileServer(http.FS(staticFS))

    // SPA fallback: any non-API, non-asset request gets index.html
    // This makes React Router work (client-side routing)
    s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        // If the file exists in static/, serve it
        path := r.URL.Path
        if path == "/" {
            path = "index.html"
        }
        if _, err := staticFS.Open(path[1:]); err == nil {
            fileServer.ServeHTTP(w, r)
            return
        }
        // For any other path (React Router routes), serve index.html
        r.URL.Path = "/"
        fileServer.ServeHTTP(w, r)
    })
}
```

### 4.2 CORS headers for development mode

During development, Vite runs at localhost:5173 and the Go backend runs at localhost:3001. The Vite proxy handles this, but if testing without Vite, add CORS:

```go
func corsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
        if r.Method == "OPTIONS" {
            w.WriteHeader(http.StatusOK)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

---

## 5. Build and Verify

### 5.1 Full build sequence

```bash
# 1. Build the React dashboard
cd dashboard
npm install
npm run build
cd ..

# 2. Verify static files are in the right place
ls internal/dashboard/static/index.html
# Should exist

# 3. Build the Go binary (now includes embedded dashboard)
go build -o universe ./cmd/universe

# 4. Verify the binary size increased (dashboard adds ~200-500KB)
ls -lh universe
```

### 5.2 Verify dashboard serves correctly

```bash
# Start PostgreSQL if not running
docker compose up -d

# Configure database
./universe config set db postgres://universe_admin:universe_secret_2024@localhost:5432/universe

# Start the dashboard
./universe dashboard --port 3001
```

Open http://localhost:3001 in browser. Verify:

- [ ] Page loads with dark theme
- [ ] Sidebar shows 6 navigation items
- [ ] Overview page shows engine status
- [ ] Clicking "Memory" navigates to /memory (React Router works)
- [ ] Memory page loads observations from the API
- [ ] Skills page shows seed skills from the database
- [ ] Browser refresh on /memory still works (SPA fallback serves index.html)

### 5.3 Verify API endpoints return data

```bash
# In another terminal while dashboard is running:

# Overview
curl -s http://localhost:3001/api/overview | head -c 200

# Memory list
curl -s "http://localhost:3001/api/memory?limit=5" | head -c 200

# Skills list
curl -s http://localhost:3001/api/skills | head -c 200

# Routing list
curl -s "http://localhost:3001/api/routing?limit=5" | head -c 200

# Graph nodes
curl -s http://localhost:3001/api/graph/nodes | head -c 200
```

Each should return valid JSON (not HTML, not 404).

---

## 6. Development Workflow

For ongoing dashboard development after initial build:

```bash
# Terminal 1: Start the Go backend (serves API)
./universe dashboard --port 3001

# Terminal 2: Start Vite dev server (serves React with hot reload)
cd dashboard
npm run dev
# Opens at localhost:5173, API calls proxy to localhost:3001

# Edit React code → browser updates instantly
# Edit Go handlers → restart the Go binary
```

When ready to ship:

```bash
# Rebuild React → Go binary
cd dashboard && npm run build && cd ..
go build -o universe ./cmd/universe
```

---

## 7. Acceptance Criteria

- [ ] `cd dashboard && npm install` succeeds
- [ ] `cd dashboard && npm run build` produces files in `internal/dashboard/static/`
- [ ] `go build ./...` compiles with embedded static files
- [ ] `./universe dashboard` starts web server on port 3001
- [ ] Browser shows the dashboard with sidebar navigation
- [ ] All 6 pages load without JavaScript errors
- [ ] API endpoints return JSON data from PostgreSQL
- [ ] SPA routing works (browser refresh on /memory serves the app)
- [ ] Vite dev mode proxies API calls correctly
- [ ] Dashboard works with no data (empty state, not crashes)

---

## 8. What NOT to Build

- Do NOT add WebSocket — polling every 30 seconds is fine for V1
- Do NOT add authentication — localhost only, no login needed
- Do NOT add mobile layout — desktop only
- Do NOT add data export — copy/paste from browser
- Do NOT spend time on pixel-perfect design — functional and readable is enough for V1
