import { ReactNode } from "react";

interface MetricCardProps {
  label: string;
  value: string;
  icon: ReactNode;
}

export default function MetricCard({ label, value, icon }: MetricCardProps) {
  return (
    <div className="bg-[var(--color-cyber-card)] border border-[var(--color-cyber-border)] rounded-xl p-5 hover:border-cyan-500/30 transition-colors duration-300">
      <div className="flex items-center justify-between mb-3">
        <span className="text-xs text-slate-400 uppercase tracking-wider">{label}</span>
        <span className="text-xl">{icon}</span>
      </div>
      <p className="text-3xl font-bold text-slate-100 font-mono">{value}</p>
    </div>
  );
}

