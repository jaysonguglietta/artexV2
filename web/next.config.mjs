import { fileURLToPath } from "node:url";

// NEXT_EXPORT=1 emits static files in web/out. Development keeps API rewrites and hot reload.
const isExport = process.env.NEXT_EXPORT === "1";
// The optional mock demo has no backend or API rewrites.
const isMock = process.env.NEXT_PUBLIC_MOCK === "1";

/** @type {import('next').NextConfig} */
const nextConfig = {
  // Keep parent lockfiles from changing root inference and asset paths.
  turbopack: { root: fileURLToPath(new URL(".", import.meta.url)) },
  reactCompiler: true,
  // Development assets and HMR are restricted to local origins.
  allowedDevOrigins: ["localhost", "127.0.0.1"],
  compiler: {
    removeConsole: process.env.NODE_ENV === "production",
  },
  ...(isExport
    ? {
        // Static deployment has no Node server, image optimizer, or dynamic routes.
        output: "export",
        images: { unoptimized: true },
        trailingSlash: true,
      }
    : isMock
      ? {
          // The mock demo does not need API rewrites.
          images: { unoptimized: true },
        }
      : {
          // Development proxies /api/* to Go; AUTOPENTEST_API overrides the backend URL.
          async rewrites() {
            const backend = process.env.AUTOPENTEST_API ?? "http://localhost:8787";
            return [{ source: "/api/:path*", destination: `${backend}/api/:path*` }];
          },
        }),
};

// English messages are provided directly by the React IntlRuntimeProvider.
export default nextConfig;
