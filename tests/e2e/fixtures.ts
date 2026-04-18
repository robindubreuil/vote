export function generateSessionCode(): string {
  return Math.floor(1000 + Math.random() * 9000).toString();
}

export const TEST_COLORS = {
  red: '#ef4444',
  blue: '#3b82f6',
  green: '#22c55e',
  yellow: '#eab308',
  orange: '#f97316',
  violet: '#a855f7',
  rose: '#ec4899',
  gray: '#6b7280',
} as const;
