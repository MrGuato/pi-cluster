import { ReactNode } from "react";

interface InfraCardProps {
  icon: ReactNode;
  label: string;
  value: string;
  sublabel?: string;
}

export default function InfraCard({ icon, label, value, sublabel }: InfraCardProps) {
  return (
    <div className="bg-[var(--color-cyber-card)] border border-[var(--color-cyber-border)] rounded-xl p-4 hover:border-cyan-500/30 transition-colors duration-300">
      <div className="flex items-center gap-3 mb-2">
        <div className="text-2xl">{icon}</div>
        <div>
          <p className="text-xs text-slate-400 uppercase tracking-wider">{label}</p>
          <p className="text-sm font-semibold text-slate-100">{value}</p>
        </div>
      </div>
      {sublabel && (
        <p className="text-xs text-slate-500 mt-1">{sublabel}</p>
      )}
    </div>
  );
}

