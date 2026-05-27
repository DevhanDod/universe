import './styles.css';
import { Routes, Route } from 'react-router-dom';
import Layout from './components/Layout.jsx';
import Overview from './pages/Overview.jsx';
import Graph from './pages/Graph.jsx';
import Memory from './pages/Memory.jsx';
import Skills from './pages/Skills.jsx';
import Plans from './pages/Plans.jsx';
import Compression from './pages/Compression.jsx';
import Activity from './pages/Activity.jsx';

export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/"            element={<Overview />} />
        <Route path="/graph"       element={<Graph />} />
        <Route path="/memory"      element={<Memory />} />
        <Route path="/skills"      element={<Skills />} />
        <Route path="/plans"       element={<Plans />} />
        <Route path="/compression" element={<Compression />} />
        <Route path="/activity"    element={<Activity />} />
      </Route>
    </Routes>
  );
}
