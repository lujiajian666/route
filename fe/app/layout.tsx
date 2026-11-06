import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "静态路由守护台 | RouteKeeper",
  description: "托管主机路由，自动巡检修复，并通过 route -n get 核验网关走向。",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="zh-CN" className="h-full antialiased">
      <body className="flex min-h-full flex-col bg-[#08090c] text-zinc-200">{children}</body>
    </html>
  );
}
