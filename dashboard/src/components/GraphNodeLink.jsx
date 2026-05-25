import { useNavigate } from 'react-router-dom';

export default function GraphNodeLink({ nodeId, onClick }) {
  const navigate = useNavigate();

  if (!nodeId) return null;

  const display = nodeId.length > 40 ? nodeId.slice(0, 37) + '…' : nodeId;

  function handleClick(e) {
    e.stopPropagation();
    if (onClick) {
      onClick(nodeId);
    } else {
      navigate(`/graph?node=${encodeURIComponent(nodeId)}`);
    }
  }

  return (
    <span className="graph-node-link" onClick={handleClick} title={nodeId}>
      {display}
    </span>
  );
}
