import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "静态路由管理 | RouteKeeper",
  description: "添加、删除、巡检主机路由，并通过 route -n get 核验网关。",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="zh-CN" className="h-full antialiased">
      <body className="flex min-h-full flex-col bg-[#08090c] text-zinc-200">{children}</body>
    </html>
  );
}
