type Bucket = {
  count: number;
  resetAt: number;
};

const globalForRateLimit = globalThis as typeof globalThis & {
  __themistoRateLimitBuckets?: Map<string, Bucket>;
};

function getBuckets() {
  if (!globalForRateLimit.__themistoRateLimitBuckets) {
    globalForRateLimit.__themistoRateLimitBuckets = new Map<string, Bucket>();
  }
  return globalForRateLimit.__themistoRateLimitBuckets;
}

export function takeRateLimit(key: string, limit: number, windowMs: number) {
  const buckets = getBuckets();
  const now = Date.now();
  const existing = buckets.get(key);
  if (!existing || existing.resetAt <= now) {
    buckets.set(key, { count: 1, resetAt: now + windowMs });
    return { allowed: true, retryAfterSeconds: 0 };
  }

  if (existing.count >= limit) {
    return {
      allowed: false,
      retryAfterSeconds: Math.max(1, Math.ceil((existing.resetAt - now) / 1000))
    };
  }

  existing.count += 1;
  buckets.set(key, existing);
  return { allowed: true, retryAfterSeconds: 0 };
}
