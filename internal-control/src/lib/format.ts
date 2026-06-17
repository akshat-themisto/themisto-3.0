import { format, formatDistanceToNow } from "date-fns";

export function formatDateTime(value?: string | Date | null) {
  if (!value) return "-";
  return format(new Date(value), "dd MMM yyyy, h:mm a");
}

export function formatCurrency(cents: number) {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: 0
  }).format(cents / 100);
}

export function formatRelative(value?: string | Date | null) {
  if (!value) return "No recent activity";
  return formatDistanceToNow(new Date(value), { addSuffix: true });
}

export function formatStatusReason(value?: string | null) {
  if (!value) return "";
  if (value.startsWith("Imported from local runtime:")) return "";
  return value;
}

export function cn(...values: Array<string | false | null | undefined>) {
  return values.filter(Boolean).join(" ");
}

