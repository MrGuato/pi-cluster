import type { StatusLevel } from "@/lib/types";

interface StatusDotProps {
  status: StatusLevel;
  label: string;
  detail: string;
}

const dotStyles: Record<StatusLevel, { bg: string; animation: string }> = {
  healthy: {
    bg: "bg-green-500",
    animation: "pulse-green 2s ease-in-out infinite",
  },
  warning: {
    bg: "bg-yellow-500",
    animation: "pulse-yellow 2s ease-in-out infinite",
  },
  degraded: {
    bg: "bg-red-500",
    animation: "pulse-red 2s ease-in-out infinite",
  },
  unknown: {
    bg: "bg-gray-500",
    animation: "pulse-gray 3s ease-in-out infinite",
  },
};

export default function StatusDot({ status, label, detail }: StatusDotProps) {
  const style = dotStyles[status];

  return (
    <div className="flex items-center gap-2 group relative">
      <span
        className={`w-3 h-3 rounded-full inline-block ${style.bg}`}
        style={{ animation: style.animation }}
      />
      <span className="text-sm text-slate-300">{label}</span>
      <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 px-3 py-1.5 bg-gray-800 text-xs text-slate-300 rounded-lg opacity-0 group-hover:opacity-100 transition-opacity whitespace-nowrap pointer-events-none border border-gray-700">
        {detail}
      </div>
    </div>
  );
}

