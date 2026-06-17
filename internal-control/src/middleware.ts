import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

const sessionCookieName = process.env.SESSION_COOKIE_NAME ?? "themisto_internal_session";

export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;
  const applySecurityHeaders = (response: NextResponse) => {
    response.headers.set("X-Frame-Options", "DENY");
    response.headers.set("X-Content-Type-Options", "nosniff");
    response.headers.set("Referrer-Policy", "no-referrer");
    response.headers.set("Permissions-Policy", "camera=(), microphone=(), geolocation=()");
    response.headers.set("Cross-Origin-Opener-Policy", "same-origin");
    response.headers.set("Cache-Control", "no-store");
    return response;
  };

  if (
    pathname.startsWith("/_next") ||
    pathname.startsWith("/favicon") ||
    pathname.startsWith("/api/v1") ||
    pathname === "/login"
  ) {
    return applySecurityHeaders(NextResponse.next());
  }

  const token = request.cookies.get(sessionCookieName)?.value;
  if (!token) {
    const url = request.nextUrl.clone();
    url.pathname = "/login";
    return applySecurityHeaders(NextResponse.redirect(url));
  }

  return applySecurityHeaders(NextResponse.next());
}

export const config = {
  matcher: ["/((?!.*\\..*|_next).*)"]
};

