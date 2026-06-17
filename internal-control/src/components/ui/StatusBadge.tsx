import { cn } from "@/lib/format";

type StatusBadgeProps = {
  status: string;
  className?: string;
};

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const normalized = status.toLowerCase();
  return <span className={cn("badge", normalized, className)}>{status.replace(/_/g, " ")}</span>;
}

