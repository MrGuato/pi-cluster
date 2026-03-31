import type { NodeInfo } from "@/lib/types";
import ProgressBar from "./ProgressBar";

interface NodeCardProps {
  node: NodeInfo;
}

function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  if (days > 0) return `${days}d ${hours}h`;
  const minutes = Math.floor((seconds % 3600) / 60);
  return `${hours}h ${minutes}m`;
}

export default function NodeCard({ node }: NodeCardProps) {
  return (
    <div className="bg-[var(--color-cyber-card)] border border-[var(--color-cyber-border)] rounded-xl p-5 hover:border-cyan-500/30 transition-colors duration-300">
      <div className="flex items-center justify-between mb-4">
        <div>
          <h3 className="text-sm font-semibold text-slate-100">{node.name}</h3>
          <p className="text-xs text-slate-500">{node.role}</p>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={`w-2.5 h-2.5 rounded-full ${node.ready ? "bg-green-500" : "bg-red-500"}`}
          />
          <span className={`text-xs ${node.ready ? "text-green-400" : "text-red-400"}`}>
            {node.ready ? "Ready" : "NotReady"}
          </span>
        </div>
      </div>

      <div className="space-y-3">
        <ProgressBar label="CPU" value={node.cpuPercent} />
        <ProgressBar label="Memory" value={node.memoryPercent} />
      </div>

      <div className="flex gap-4 mt-4 pt-3 border-t border-gray-800">
        <div>
          <p className="text-xs text-slate-500">Pods</p>
          <p className="text-sm font-mono text-slate-300">{node.pods}</p>
        </div>
        <div>
          <p className="text-xs text-slate-500">Uptime</p>
          <p className="text-sm font-mono text-slate-300">{formatUptime(node.uptimeSeconds)}</p>
        </div>
      </div>
    </div>
  );
}

