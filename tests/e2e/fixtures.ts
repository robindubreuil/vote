const SESSION_ALPHABET = 'ABCDEFGHJKLMNPQRSTUVWXY';

export function generateSessionCode(): string {
  let code = '';
  for (let i = 0; i < 3; i++) {
    code += SESSION_ALPHABET[Math.floor(Math.random() * SESSION_ALPHABET.length)];
  }
  return code;
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
