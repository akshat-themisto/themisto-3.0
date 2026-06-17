import { timingSafeEqual } from "crypto";

export function verifyInternalApiToken(candidate: string | null) {
  const expected = process.env.INTERNAL_API_TOKEN;
  if (!expected || !candidate) return false;
  const expectedBuffer = Buffer.from(expected);
  const candidateBuffer = Buffer.from(candidate);
  if (expectedBuffer.length !== candidateBuffer.length) return false;
  return timingSafeEqual(expectedBuffer, candidateBuffer);
}

