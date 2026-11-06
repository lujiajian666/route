"use client";

import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";

import {
  addRoute,
  applyRoute,
  deleteRoute,
  errorMessage,
  fetchRouteTable,
  fetchRoutes,
  type RouteTableResponse,
  type RouteView,
  type RoutesResponse,
  type RouteStatus,
} from "@/app/lib/api";

const REFRESH_MS = 5000;
const IPV4_RE = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;

const STATUS: Record<RouteStatus, { label: string; dot: string; badge: string }> = {
  ok: { label: "正常", dot: "bg-emerald-400", badge: "border-emerald-400/30 bg-emerald-400/10 text-emerald-300" },
  missing: { label: "缺失", dot: "bg-amber-400", badge: "border-amber-400/30 bg-amber-400/10 text-amber-300" },
  gateway_mismatch: { label: "网关不符", dot: "bg-orange-400", badge: "border-orange-400/30 bg-orange-400/10 text-orange-300" },
  error: { label: "失败", dot: "bg-rose-400", badge: "border-rose-400/30 bg-rose-400/10 text-rose-300" },
};

const INPUT =
  "h-11 w-full rounded-lg border border-white/10 bg-black/30 px-3 font-mono text-sm text-white outline-none placeholder:text-zinc-600 focus:border-cyan-400/60";

export default function RouteDashboard() {
  const [data, setData] = useState<RoutesResponse | null>(null);
  const [table, setTable] = useState<RouteTableResponse | null>(null);
  const [host, setHost] = useState("");
  const [gateway, setGateway] = useState("");
  const [formError, setFormError] = useState<string | null>(null);
  const [connError, setConnError] = useState<string | null>(null);
  const [notice, setNotice] = useState<{ kind: "ok" | "err"; text: string } | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);
  const inFlight = useRef(false);

  const load = useCallback(async () => {
    if (inFlight.current) return;
    inFlight.current = true;
    try {
      const [routes, routeTable] = await Promise.all([fetchRoutes(), fetchRouteTable()]);
      setData(routes);
      setTable(routeTable);
      setConnError(null);
      setLastRefresh(new Date());
    } catch (error) {
      setConnError(errorMessage(error));
    } finally {
      inFlight.current = false;
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const kickoff = window.setTimeout(() => void load(), 0);
    const timer = window.setInterval(() => void load(), REFRESH_MS);
    return () => {
      window.clearTimeout(kickoff);
      window.clearInterval(timer);
    };
  }, [load]);

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), 5000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  async function handleAdd(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(null);
    const target = host.trim();
    const nextHop = gateway.trim();

    if (!IPV4_RE.test(target)) {
      setFormError("目标地址必须是合法的 IPv4，例如 5.5.2.13");
      return;
    }
    if (!IPV4_RE.test(nextHop)) {
      setFormError("网关必须是合法的 IPv4，例如 192.168.100.254");
      return;
    }
    if (target === nextHop) {
      setFormError("网关不能与目标地址相同");
      return;
    }

    setBusy("add");
    try {
      const result = await addRoute(target, nextHop);
      setNotice({ kind: "ok", text: `已${result.created ? "添加" : "更新"} ${target} → ${nextHop}` });
      setHost("");
      setGateway("");
    } catch (error) {
      setNotice({ kind: "err", text: errorMessage(error) });
    } finally {
      setBusy(null);
      await load();
    }
  }

  async function handleApply(route: RouteView) {
    setBusy(route.host);
    try {
      await applyRoute(route.host);
      setNotice({ kind: "ok", text: `${route.host} 已重建并核验` });
    } catch (error) {
      setNotice({ kind: "err", text: errorMessage(error) });
    } finally {
      setBusy(null);
      await load();
    }
  }

  async function handleDelete(route: RouteView) {
    if (!window.confirm(`确定删除 ${route.host} → ${route.gateway} 的主机路由？`)) return;
    setBusy(route.host);
    try {
      await deleteRoute(route.host);
      setNotice({ kind: "ok", text: `已删除 ${route.host}` });
    } catch (error) {
      setNotice({ kind: "err", text: errorMessage(error) });
    } finally {
      setBusy(null);
      await load();
    }
  }

  if (loading && !data) {
    return <div className="grid min-h-screen place-items-center bg-[#0a0b0e] text-sm text-zinc-400">加载中…</div>;
  }

  const summary = data?.summary ?? { total: 0, ok: 0, missing: 0, gatewayMismatch: 0, error: 0 };
  const abnormal = summary.missing + summary.gatewayMismatch + summary.error;

  return (
    <div className="min-h-screen bg-[#0a0b0e] text-zinc-200">
      <main className="mx-auto w-full max-w-5xl px-5 py-10">
        <header className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 className="text-2xl font-semibold text-white">静态路由管理</h1>
            <p className="mt-1 text-sm text-zinc-500">
              主机路由自动巡检 · route -n get 核验网关
            </p>
          </div>
          <div className="flex items-center gap-3 text-xs text-zinc-400">
            <span className="inline-flex items-center gap-1.5">
              <span className={`h-1.5 w-1.5 rounded-full ${connError ? "bg-rose-400" : "bg-emerald-400"}`} />
              {connError ? "后端离线" : "后端在线"}
            </span>
            <span className="tabular-nums">{lastRefresh ? lastRefresh.toLocaleTimeString("zh-CN", { hour12: false }) : "--:--:--"}</span>
            <button
              type="button"
              onClick={() => void load()}
              className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-1.5 text-zinc-300 hover:bg-white/[0.07]"
            >
              刷新
            </button>
          </div>
        </header>

        {connError ? (
          <p className="mt-5 rounded-lg border border-rose-400/20 bg-rose-400/[0.06] px-3 py-2 text-sm text-rose-200">
            无法连接后端：{connError}
          </p>
        ) : null}

        {notice ? (
          <p
            className={`mt-5 rounded-lg border px-3 py-2 text-sm ${
              notice.kind === "ok"
                ? "border-emerald-400/20 bg-emerald-400/[0.06] text-emerald-200"
                : "border-rose-400/20 bg-rose-400/[0.06] text-rose-200"
            }`}
          >
            {notice.text}
          </p>
        ) : null}

        <section className="mt-6 rounded-xl border border-white/10 bg-white/[0.03] p-5">
          <h2 className="text-base font-semibold text-white">添加路由</h2>
          <p className="mt-1 text-xs text-zinc-500">
            添加后立即执行删除旧主机路由、添加新路由并核验；之后每 {REFRESH_MS / 1000} 秒自动巡检。
          </p>
          <form onSubmit={handleAdd} className="mt-4 grid gap-3 sm:grid-cols-[1fr_1fr_auto]">
            <label className="block">
              <span className="mb-1.5 block text-xs text-zinc-500">目标地址</span>
              <input
                value={host}
                onChange={(event) => setHost(event.target.value)}
                placeholder="5.5.2.13"
                autoComplete="off"
                spellCheck={false}
                className={INPUT}
              />
            </label>
            <label className="block">
              <span className="mb-1.5 block text-xs text-zinc-500">网关</span>
              <input
                value={gateway}
                onChange={(event) => setGateway(event.target.value)}
                placeholder="192.168.100.254"
                autoComplete="off"
                spellCheck={false}
                className={INPUT}
              />
            </label>
            <button
              type="submit"
              disabled={busy !== null}
              className="h-11 self-end rounded-lg bg-cyan-400 px-5 text-sm font-semibold text-[#041016] hover:bg-cyan-300 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {busy === "add" ? "添加中…" : "添加"}
            </button>
          </form>
          {formError ? <p className="mt-2 text-xs text-rose-300">{formError}</p> : null}
          <p className="mt-3 font-mono text-[11px] text-zinc-600">
            route delete -host &lt;目标&gt; && route add -host &lt;目标&gt; &lt;网关&gt;
          </p>
        </section>

        <section className="mt-6 rounded-xl border border-white/10 bg-white/[0.03]">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/10 px-5 py-4">
            <h2 className="text-base font-semibold text-white">托管路由</h2>
            <p className="text-xs text-zinc-500">
              共 {summary.total} 条 · 正常 {summary.ok} · 异常 {abnormal}
            </p>
          </div>

          {summary.total === 0 ? (
            <p className="px-5 py-10 text-center text-sm text-zinc-500">还没有托管路由，先在上方添加一条。</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[720px] text-left text-sm">
                <thead className="text-xs text-zinc-500">
                  <tr className="border-b border-white/10">
                    <th className="px-5 py-3 font-medium">目标地址</th>
                    <th className="px-4 py-3 font-medium">网关</th>
                    <th className="px-4 py-3 font-medium">当前网关</th>
                    <th className="px-4 py-3 font-medium">接口</th>
                    <th className="px-4 py-3 font-medium">状态</th>
                    <th className="px-5 py-3 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/[0.06]">
                  {data?.routes.map((route) => {
                    const status = STATUS[route.status] ?? STATUS.error;
                    const working = busy === route.host;
                    return (
                      <tr key={route.host}>
                        <td className="px-5 py-4 font-mono text-white">{route.host}</td>
                        <td className="px-4 py-4 font-mono text-zinc-300">{route.gateway}</td>
                        <td className={`px-4 py-4 font-mono ${route.currentGateway === route.gateway ? "text-emerald-300" : "text-amber-300"}`}>
                          {route.currentGateway || "—"}
                        </td>
                        <td className="px-4 py-4 font-mono text-xs text-zinc-400">{route.interface || "—"}</td>
                        <td className="px-4 py-4">
                          <span
                            className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs ${status.badge}`}
                            title={route.message}
                          >
                            <span className={`h-1.5 w-1.5 rounded-full ${status.dot}`} />
                            {status.label}
                          </span>
                          <p className="mt-1 max-w-[220px] truncate text-[11px] text-zinc-500" title={route.message}>
                            {route.message}
                          </p>
                        </td>
                        <td className="px-5 py-4">
                          <div className="flex justify-end gap-2">
                            <button
                              type="button"
                              onClick={() => void handleApply(route)}
                              disabled={busy !== null}
                              className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-1.5 text-xs text-zinc-300 hover:bg-white/[0.07] disabled:opacity-40"
                            >
                              {working ? "处理中…" : "修复"}
                            </button>
                            <button
                              type="button"
                              onClick={() => void handleDelete(route)}
                              disabled={busy !== null}
                              className="rounded-lg border border-rose-400/20 bg-rose-400/[0.06] px-3 py-1.5 text-xs text-rose-300 hover:bg-rose-400/10 disabled:opacity-40"
                            >
                              删除
                            </button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </section>

        <section className="mt-6 rounded-xl border border-white/10 bg-white/[0.03] p-5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h2 className="text-base font-semibold text-white">路由表</h2>
            <p className="font-mono text-[11px] text-zinc-500">{table?.command ?? "netstat -rn"}</p>
          </div>
          {table?.error ? <p className="mt-2 text-xs text-amber-300">读取路由表失败：{table.error}</p> : null}
          <pre className="mt-3 max-h-[420px] overflow-auto rounded-lg border border-white/10 bg-black/40 p-4 font-mono text-[11.5px] leading-relaxed text-zinc-300">
            {table?.raw || "加载中…"}
          </pre>
        </section>

        <footer className="mt-8 text-center text-[11px] text-zinc-600">
          修改路由表需要 root 权限，请使用 sudo 启动后端
        </footer>
      </main>
    </div>
  );
}
