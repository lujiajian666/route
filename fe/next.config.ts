import type { NextConfig } from "next";

// The Go backend runs separately; Next proxies /api/* to it so the browser only
// ever talks to one origin (no CORS setup required).
const backendOrigin = (
  process.env.BACKEND_ORIGIN ?? "http://127.0.0.1:8080"
).replace(/\/+$/, "");

const nextConfig: NextConfig = {
  experimental: {
    agentFeedback: true,
  },
  cacheComponents: true,
  partialPrefetching: true,
  allowedDevOrigins: ["127.0.0.1"],
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${backendOrigin}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
