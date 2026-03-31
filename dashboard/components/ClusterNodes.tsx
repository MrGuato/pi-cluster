import NodeCard from "./NodeCard";
import type { NodeInfo } from "@/lib/types";

interface ClusterNodesProps {
  nodes: NodeInfo[] | null;
}

export default function ClusterNodes({ nodes }: ClusterNodesProps) {
  return (
    <section>
      <h2 className="text-xs font-semibold text-slate-500 uppercase tracking-widest mb-4">
        Cluster Nodes
      </h2>
      {nodes === null ? (
        <div className="text-sm text-slate-500">Loading node data...</div>
      ) : nodes.length === 0 ? (
        <div className="text-sm text-slate-500">No nodes found</div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
          {nodes.map((node) => (
            <NodeCard key={node.name} node={node} />
          ))}
        </div>
      )}
    </section>
  );
}

