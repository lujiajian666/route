"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";

import {
  addManagedRoute,
  applyManagedRoute,
  deleteManagedRoute,
  errorMessage,
  fetchLogs,
  fetchRouteTable,
  fetchRoutes,
  fetchSystem,
  reconcileAllRoutes,
  updateSettings,
  type RouteEvent,
  type RouteStatus,
  type RouteTableResponse,
  type RouteView,
  type RoutesResponse,
  type Settings,
  type SystemInfo,
} from "@/app/lib/api";

const POLL_MS = 4000;
const TABLE_POLL_MS = 6000;

const STATUS_META: Record<
  RouteStatus,
  { label: string; dot: string; badge: string }
> = {
  ok: {
    label: "正常",
    dot: "bg-emerald-400",
    badge: "border-emerald-400/25 bg-emerald-400/10 text-emerald-300",
  },
  missing: {
    label: "缺失",
    dot: "bg-amber-400",
    badge: "border-amber-400/25 bg-amber-400/10 text-amber-300",
  },
  gateway_mismatch: {
    label: "网关不符",
    dot: "bg-orange-400",
    badge: "border-orange-400/25 bg-orange-400/10 text-orange-300",
  },
  error: {
    label: "检测失败",
    dot: "bg-rose-400",
    badge: "border-rose-400/25 bg-rose-400/10 text-rose-300",
  },
};

const LEVEL_META: Record<RouteEvent["level"], { label: string; text: string; dot: string }> = {
  info: { label: "信息", text: "text-sky-300", dot: "bg-sky-400" },
  success: { label: "成功", text: "text-emerald-300", dot: "bg-emerald-400" },
  warn: { label: "警告", text: "text-amber-300", dot: "bg-amber-400" },
  error: { label: "错误", text: "text-rose-300", dot: "bg-rose-400" },
};

const ACTION_LABEL: Record<string, string> = {
  add: "添加",
  update: "更新",
  delete: "删除",
  reconcile: "巡检",
  settings: "设置",
  inspect: "检测",
  verify: "核验",
};

const INPUT_CLASS =
  "h-[46px] w-full rounded-xl border border-white/10 bg-black/30 px-3.5 font-mono text-sm text-white outline-none transition placeholder:text-zinc-600 focus:border-cyan-400/60 focus:ring-2 focus:ring-cyan-400/10";

const GHOST_BUTTON =
  "inline-flex items-center justify-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.03] px-2.5 py-1.5 text-xs font-medium text-zinc-300 transition hover:border-white/20 hover:bg-white/[0.07] hover:text-white disabled:cursor-not-allowed disabled:opacity-40";

const IPV4_RE =
  /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;

type ToastState = { id: number; kind: "success" | "error" | "info"; text: string };

export default function RouteDashboard() {
  const [data, setData] = useState<RoutesResponse | null>(null);
  const [system, setSystem] = useState<SystemInfo | null>(null);
  const [logs, setLogs] = useState<RouteEvent[]>([]);
  const [table, setTable] = useState<RouteTableResponse | null>(null);
  const [tableOpen, setTableOpen] = useState(true);
  const [tableLoading, setTableLoading] = useState(false);
  const [loading, setLoading] = useState(true);
  const [online, setOnline] = useState<boolean | null>(null);
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);
  const [connectionError, setConnectionError] = useState<string | null>(null);

  const [host, setHost] = useState("");
  const [gateway, setGateway] = useState("");
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [toast, setToast] = useState<ToastState | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<RouteView | null>(null);

  const inFlight = useRef(false);
  const tableInFlight = useRef(false);
  const toastSeq = useRef(0);

  const refresh = useCallback(async (silent = false) => {
    if (inFlight.current) return;
    inFlight.current = true;
    try {
      const [routesRes, systemRes, logsRes] = await Promise.all([
        fetchRoutes(),
        fetchSystem(),
        fetchLogs(80),
      ]);
      setData(routesRes);
      setSystem(systemRes);
      setLogs(logsRes.logs);
      setOnline(true);
      setConnectionError(null);
      setLastRefresh(new Date());
    } catch (error) {
      setOnline(false);
      if (!silent) setConnectionError(errorMessage(error));
    } finally {
      inFlight.current = false;
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const kickoff = window.setTimeout(() => void refresh(), 0);
    const timer = window.setInterval(() => void refresh(true), POLL_MS);
    return () => {
      window.clearTimeout(kickoff);
      window.clearInterval(timer);
    };
  }, [refresh]);

  const loadTable = useCallback(async (silent = false) => {
    if (tableInFlight.current) return;
    tableInFlight.current = true;
    if (!silent) setTableLoading(true);
    try {
      setTable(await fetchRouteTable());
    } catch (error) {
      if (!silent) {
        toastSeq.current += 1;
        setToast({ id: toastSeq.current, kind: "error", text: errorMessage(error) });
      }
    } finally {
      tableInFlight.current = false;
      if (!silent) setTableLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!tableOpen) return;
    const kickoff = window.setTimeout(() => void loadTable(), 0);
    const timer = window.setInterval(() => void loadTable(true), TABLE_POLL_MS);
    return () => {
      window.clearTimeout(kickoff);
      window.clearInterval(timer);
    };
  }, [tableOpen, loadTable]);

  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), 5200);
    return () => window.clearTimeout(timer);
  }, [toast]);

  useEffect(() => {
    if (!deleteTarget) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setDeleteTarget(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [deleteTarget]);

  const settings: Settings = system?.settings ?? data?.settings ?? {
    autoHeal: false,
    intervalSeconds: 5,
  };
  const summary = data?.summary ?? {
    total: 0,
    ok: 0,
    missing: 0,
    gatewayMismatch: 0,
    error: 0,
  };
  const abnormal = summary.missing + summary.gatewayMismatch + summary.error;
  const dryRun = system?.dryRun ?? data?.dryRun ?? false;
  const intervalOptions = useMemo(() => {
    const base = [3, 5, 10, 15, 30, 60, 120];
    return base.includes(settings.intervalSeconds)
      ? base
      : [...base, settings.intervalSeconds].sort((a, b) => a - b);
  }, [settings.intervalSeconds]);

  const addPreview = useMemo(() => {
    const hostPart = host.trim() || "<host>";
    const gatewayPart = gateway.trim() || "<gateway>";
    if (system?.commands?.add) {
      return system.commands.add
        .replace("<host>", hostPart)
        .replace("<gateway>", gatewayPart);
    }
    return `sudo route add -host ${hostPart} ${gatewayPart}`;
  }, [host, gateway, system]);

  const deletePreview = useMemo(() => {
    const hostPart = host.trim() || "<host>";
    if (system?.commands?.delete) {
      return system.commands.delete.replace("<host>", hostPart);
    }
    return `sudo route delete -host ${hostPart}`;
  }, [host, system]);

  function showToast(kind: ToastState["kind"], text: string) {
    toastSeq.current += 1;
    setToast({ id: toastSeq.current, kind, text });
  }

  async function handleAdd(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const target = host.trim();
    const nextHop = gateway.trim();
    setFormError(null);

    if (!target) {
      setFormError("请填写目标地址");
      return;
    }
    if (!IPV4_RE.test(target)) {
      setFormError("目标地址必须是合法的 IPv4，例如 5.5.2.13");
      return;
    }
    if (!nextHop) {
      setFormError("请填写下一跳网关");
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
    if (nextHop === "0.0.0.0" || target === "0.0.0.0") {
      setFormError("地址不能是 0.0.0.0");
      return;
    }

    setBusy("add");
    try {
      const response = await addManagedRoute(target, nextHop);
      showToast(
        "success",
        `${response.created ? "已添加" : "已更新"}托管路由 ${target} → ${nextHop}，${response.apply.message}`,
      );
      setHost("");
      setGateway("");
      await refresh();
    } catch (error) {
      setFormError(errorMessage(error));
      await refresh(true);
    } finally {
      setBusy(null);
    }
  }

  async function handleApply(route: RouteView) {
    if (busy) return;
    setBusy(`apply:${route.host}`);
    try {
      const response = await applyManagedRoute(route.host);
      const kind = response.apply.state.status === "ok" ? "success" : "error";
      showToast(kind, `${route.host}：${response.apply.message}`);
      await refresh();
    } catch (error) {
      showToast("error", errorMessage(error));
      await refresh(true);
    } finally {
      setBusy(null);
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return;
    const target = deleteTarget;
    setBusy("delete");
    try {
      const response = await deleteManagedRoute(target.host);
      showToast(
        "info",
        response.result.warnings?.length
          ? `已停止托管 ${target.host}，但内核删除有告警：${response.result.warnings.join("；")}`
          : response.result.message,
      );
      setDeleteTarget(null);
      await refresh();
    } catch (error) {
      showToast("error", errorMessage(error));
      setDeleteTarget(null);
      await refresh(true);
    } finally {
      setBusy(null);
    }
  }

  async function handleReconcile() {
    if (busy) return;
    setBusy("reconcile");
    try {
      const response = await reconcileAllRoutes();
      const repaired = response.results.filter((item) => item.changed).length;
      const parts = [`巡检完成：${response.summary.ok}/${response.summary.total} 正常`];
      if (repaired > 0) parts.push(`已修复 ${repaired} 条`);
      if (response.summary.missing + response.summary.gatewayMismatch > 0) {
        parts.push(`${response.summary.missing + response.summary.gatewayMismatch} 条仍异常`);
      }
      if (response.summary.error > 0) parts.push(`${response.summary.error} 条检测失败`);
      showToast(
        response.summary.total > 0 && response.summary.ok === response.summary.total ? "success" : "info",
        parts.join("，"),
      );
      await refresh();
    } catch (error) {
      showToast("error", errorMessage(error));
      await refresh(true);
    } finally {
      setBusy(null);
    }
  }

  async function handleSettings(patch: Partial<Settings>) {
    if (busy) return;
    setBusy("settings");
    try {
      const response = await updateSettings(patch);
      setSystem((prev) => (prev ? { ...prev, settings: response.settings } : prev));
      setData((prev) => (prev ? { ...prev, settings: response.settings } : prev));
      showToast("success", "守护设置已保存");
    } catch (error) {
      showToast("error", errorMessage(error));
    } finally {
      setBusy(null);
    }
  }

  async function copyTable() {
    if (!table?.raw) return;
    try {
      await navigator.clipboard.writeText(table.raw);
      showToast("success", "路由表已复制到剪贴板");
    } catch {
      showToast("error", "复制失败，请手动选择文本");
    }
  }

  if (loading && !data) {
    return (
      <div className="grid min-h-screen place-items-center bg-[#08090c] text-zinc-400">
        <div className="flex flex-col items-center gap-4">
          <div className="grid h-14 w-14 place-items-center rounded-2xl border border-cyan-400/20 bg-cyan-400/10 text-cyan-300">
            <IconRoute className="h-7 w-7" />
          </div>
          <div className="flex items-center gap-2 text-sm">
            <Spinner className="h-4 w-4 text-cyan-300" />
            正在连接 route-daemon…
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="relative min-h-screen overflow-hidden bg-[#08090c] text-zinc-200">
      <div aria-hidden className="pointer-events-none absolute inset-0">
        <div className="absolute -top-44 left-1/2 h-[520px] w-[900px] -translate-x-1/2 rounded-full bg-cyan-500/[0.07] blur-[130px]" />
        <div className="absolute top-1/3 -right-40 h-[420px] w-[420px] rounded-full bg-emerald-500/[0.05] blur-[110px]" />
        <div className="absolute inset-0 bg-[linear-gradient(rgba(255,255,255,0.018)_1px,transparent_1px),linear-gradient(90deg,rgba(255,255,255,0.018)_1px,transparent_1px)] [background-size:44px_44px] [mask-image:radial-gradient(ellipse_at_top,black,transparent_75%)]" />
      </div>

      <main className="relative mx-auto w-full max-w-[1440px] px-5 pb-14 pt-8 sm:px-8">
        <header className="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
          <div className="flex items-start gap-4">
            <div className="grid h-12 w-12 shrink-0 place-items-center rounded-2xl border border-cyan-400/20 bg-cyan-400/10 text-cyan-300 shadow-[0_0_50px_-14px_rgba(34,211,238,0.8)]">
              <IconRoute className="h-6 w-6" />
            </div>
            <div>
              <div className="flex flex-wrap items-center gap-2">
                <h1 className="text-2xl font-semibold tracking-tight text-white sm:text-[28px]">
                  静态路由守护台
                </h1>
                <span className="rounded-full border border-white/10 bg-white/[0.04] px-2 py-0.5 font-mono text-[10px] tracking-[0.16em] text-zinc-400">
                  ROUTEKEEPER
                </span>
                {dryRun ? (
                  <span className="rounded-full border border-amber-400/30 bg-amber-400/10 px-2 py-0.5 font-mono text-[10px] font-semibold tracking-wider text-amber-300">
                    DRY-RUN
                  </span>
                ) : null}
              </div>
              <p className="mt-1.5 text-sm text-zinc-400">
                托管主机路由 · 定时巡检 · 自动重建 · route -n get 双向核验
              </p>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <div className="inline-flex items-center gap-2 rounded-xl border border-white/10 bg-white/[0.03] px-3 py-2 text-xs text-zinc-400">
              <span
                className={`h-1.5 w-1.5 rounded-full ${
                  online ? "bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.9)]" : "bg-rose-400"
                }`}
              />
              <span>{online ? "后端在线" : "后端离线"}</span>
              <span className="text-zinc-600">·</span>
              <span className="tabular-nums">{lastRefresh ? formatClock(lastRefresh.toISOString()) : "--:--:--"}</span>
            </div>
            <button
              type="button"
              onClick={() => void refresh()}
              className={GHOST_BUTTON}
              title="刷新数据"
            >
              <IconRefresh className="h-3.5 w-3.5" />
              刷新
            </button>
            <button
              type="button"
              onClick={() => void handleReconcile()}
              disabled={busy !== null || !online}
              className="inline-flex items-center gap-2 rounded-xl bg-cyan-400 px-4 py-2.5 text-sm font-semibold text-[#041016] shadow-[0_10px_35px_-14px_rgba(34,211,238,0.95)] transition hover:bg-cyan-300 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {busy === "reconcile" ? <Spinner className="h-4 w-4" /> : <IconBolt className="h-4 w-4" />}
              立即巡检修复
            </button>
          </div>
        </header>

        {dryRun ? (
          <div className="mt-6 flex items-start gap-3 rounded-2xl border border-amber-400/20 bg-amber-400/[0.06] px-4 py-3 text-sm text-amber-200">
            <IconAlert className="mt-0.5 h-4 w-4 shrink-0 text-amber-300" />
            <p>
              当前为 <span className="font-semibold">dry-run 模式</span>：所有增删路由的命令只会被模拟和记录，不会真正修改内核路由表。
              移除 <code className="rounded bg-black/30 px-1 py-0.5 font-mono text-[11px]">--dry-run</code> 后重启后端即可实际生效。
            </p>
          </div>
        ) : null}

        {!online ? (
          <div className="mt-6 flex items-start gap-3 rounded-2xl border border-rose-400/20 bg-rose-400/[0.06] px-4 py-3 text-sm text-rose-200">
            <IconAlert className="mt-0.5 h-4 w-4 shrink-0 text-rose-300" />
            <div>
              <p className="font-medium">无法连接后端服务，正在每 {POLL_MS / 1000} 秒自动重试。</p>
              <p className="mt-1 text-xs text-rose-300/80">
                {connectionError ?? "请确认 route-daemon 已启动，并检查 Next.js 的 /api 代理地址。"}
              </p>
            </div>
          </div>
        ) : null}

        <section className="mt-6 grid grid-cols-2 gap-3 lg:grid-cols-4">
          <StatCard
            label="托管路由"
            value={String(summary.total)}
            hint={dryRun ? "dry-run 模式" : "持久化于后端配置文件"}
            icon={<IconServer className="h-4 w-4" />}
            tone="zinc"
          />
          <StatCard
            label="正常"
            value={String(summary.ok)}
            hint={summary.total > 0 ? `覆盖率 ${Math.round((summary.ok / summary.total) * 100)}%` : "暂无托管路由"}
            icon={<IconShield className="h-4 w-4" />}
            tone="emerald"
          />
          <StatCard
            label="异常"
            value={String(abnormal)}
            hint={
              abnormal === 0
                ? "网关与主机路由均正确"
                : `缺失 ${summary.missing} · 网关不符 ${summary.gatewayMismatch} · 失败 ${summary.error}`
            }
            icon={<IconAlert className="h-4 w-4" />}
            tone={abnormal === 0 ? "zinc" : "amber"}
          />
          <StatCard
            label="自动修复"
            value={settings.autoHeal ? "运行中" : "已暂停"}
            hint={`巡检间隔 ${settings.intervalSeconds} 秒`}
            icon={<IconBolt className="h-4 w-4" />}
            tone={settings.autoHeal ? "cyan" : "zinc"}
          />
        </section>

        <div className="mt-6 grid items-start gap-6 xl:grid-cols-[minmax(0,1.72fr)_minmax(340px,1fr)]">
          <section className="rounded-2xl border border-white/[0.07] bg-white/[0.025] p-5 sm:p-6">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <h2 className="text-base font-semibold text-white">添加 / 更新托管路由</h2>
                <p className="mt-1 text-xs text-zinc-500">
                  保存后会立即下发并核验；若路由表被重置，守护进程会自动重新添加。
                </p>
              </div>
              <span className="grid h-9 w-9 place-items-center rounded-xl border border-white/10 bg-white/[0.03] text-zinc-400">
                <IconPlus className="h-4 w-4" />
              </span>
            </div>

            <form onSubmit={handleAdd} className="mt-5 grid gap-3 lg:grid-cols-[1fr_1fr_auto]">
              <label className="block">
                <span className="mb-1.5 block text-[11px] font-medium uppercase tracking-[0.12em] text-zinc-500">
                  目标地址 / Host
                </span>
                <input
                  value={host}
                  onChange={(event) => setHost(event.target.value)}
                  placeholder="5.5.2.13"
                  inputMode="decimal"
                  autoComplete="off"
                  spellCheck={false}
                  className={INPUT_CLASS}
                />
              </label>
              <label className="block">
                <span className="mb-1.5 block text-[11px] font-medium uppercase tracking-[0.12em] text-zinc-500">
                  下一跳网关 / Gateway
                </span>
                <input
                  value={gateway}
                  onChange={(event) => setGateway(event.target.value)}
                  placeholder="192.168.100.254"
                  inputMode="decimal"
                  autoComplete="off"
                  spellCheck={false}
                  className={INPUT_CLASS}
                />
              </label>
              <button
                type="submit"
                disabled={busy !== null}
                className="inline-flex h-[46px] items-center justify-center gap-2 self-end rounded-xl bg-cyan-400 px-5 text-sm font-semibold text-[#041016] shadow-[0_10px_35px_-14px_rgba(34,211,238,0.95)] transition hover:bg-cyan-300 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {busy === "add" ? <Spinner className="h-4 w-4" /> : <IconPlus className="h-4 w-4" />}
                添加并应用
              </button>
            </form>

            {formError ? (
              <p className="mt-3 flex items-start gap-2 rounded-xl border border-rose-400/20 bg-rose-400/[0.06] px-3 py-2 text-xs text-rose-200">
                <IconAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                {formError}
              </p>
            ) : null}

            <div className="mt-4 grid gap-2 rounded-xl border border-white/[0.06] bg-black/30 p-3 font-mono text-[11px] leading-relaxed text-zinc-500 lg:grid-cols-2">
              <p className="truncate" title={addPreview}>
                <span className="text-zinc-600">$ </span>
                {addPreview}
              </p>
              <p className="truncate" title={deletePreview}>
                <span className="text-zinc-600">$ </span>
                {deletePreview}
              </p>
            </div>

            <ol className="mt-3 grid gap-2 text-[11px] text-zinc-500 sm:grid-cols-3">
              {["保存后立即下发路由", "定时读取 route -n get", "异常时自动重建并复核"].map((text, index) => (
                <li
                  key={text}
                  className="flex items-center gap-2 rounded-lg border border-white/[0.04] bg-black/20 px-2.5 py-2"
                >
                  <span className="grid h-4 w-4 shrink-0 place-items-center rounded-full bg-white/[0.06] font-mono text-[9px] text-zinc-400">
                    {index + 1}
                  </span>
                  {text}
                </li>
              ))}
            </ol>
          </section>

          <section className="rounded-2xl border border-white/[0.07] bg-white/[0.025] p-5 sm:p-6">
            <div className="flex items-start justify-between gap-3">
              <div>
                <h2 className="text-base font-semibold text-white">守护设置</h2>
                <p className="mt-1 text-xs text-zinc-500">配置会自动保存到后端，重启后仍然生效。</p>
              </div>
              <span
                className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-medium ${
                  settings.autoHeal
                    ? "border-cyan-400/25 bg-cyan-400/10 text-cyan-300"
                    : "border-white/10 bg-white/[0.04] text-zinc-400"
                }`}
              >
                <span className={`h-1.5 w-1.5 rounded-full ${settings.autoHeal ? "animate-pulse bg-cyan-400" : "bg-zinc-500"}`} />
                {settings.autoHeal ? "守护中" : "已暂停"}
              </span>
            </div>

            <div className="mt-5 flex items-center justify-between gap-4 rounded-xl border border-white/[0.06] bg-black/20 px-3.5 py-3">
              <div>
                <p className="text-sm font-medium text-zinc-200">自动巡检修复</p>
                <p className="mt-0.5 text-[11px] text-zinc-500">发现丢失或网关错误的 host 路由时自动重建</p>
              </div>
              <Switch
                checked={settings.autoHeal}
                disabled={busy !== null}
                onChange={(checked) => void handleSettings({ autoHeal: checked })}
                label="自动巡检修复"
              />
            </div>

            <div className="mt-3 flex items-center justify-between gap-4 rounded-xl border border-white/[0.06] bg-black/20 px-3.5 py-3">
              <div>
                <p className="text-sm font-medium text-zinc-200">巡检间隔</p>
                <p className="mt-0.5 text-[11px] text-zinc-500">后台每轮检查并校验所有托管路由</p>
              </div>
              <select
                value={settings.intervalSeconds}
                onChange={(event) => void handleSettings({ intervalSeconds: Number(event.target.value) })}
                disabled={busy !== null || !settings.autoHeal}
                className="h-9 rounded-lg border border-white/10 bg-[#12141a] px-2.5 font-mono text-xs text-zinc-200 outline-none transition focus:border-cyan-400/50 disabled:cursor-not-allowed disabled:opacity-50"
                aria-label="巡检间隔"
              >
                {intervalOptions.map((seconds) => (
                  <option key={seconds} value={seconds}>
                    {seconds} 秒
                  </option>
                ))}
              </select>
            </div>

            <div className="mt-5 space-y-2.5 border-t border-white/[0.06] pt-4 text-xs">
              <InfoRow
                label="运行权限"
                value={
                  !system
                    ? "—"
                    : system.isRoot
                      ? "root"
                      : !system.useSudo
                        ? "直接执行（未用 sudo）"
                        : system.sudoReady
                          ? "sudo -n 就绪"
                          : "sudo 待配置"
                }
                tone={!system ? undefined : system.isRoot || !system.useSudo || system.sudoReady ? "emerald" : "amber"}
              />
              <InfoRow
                label="平台"
                value={system ? `${system.platform} · ${system.arch}` : "—"}
              />
              <InfoRow label="主机名" value={system?.hostname ?? "—"} mono />
              <InfoRow label="监听地址" value={system?.listen ?? "—"} mono />
              <InfoRow label="后端版本" value={system ? `v${system.version} · ${system.goVersion}` : "—"} />
              <InfoRow label="路由二进制" value={system?.routeBinary ?? "—"} mono />
              <InfoRow
                label="上次巡检"
                value={
                  data?.lastReconcileAt
                    ? `${formatClock(data.lastReconcileAt)} · ${data.lastReconcileSummary?.ok ?? 0}/${data.lastReconcileSummary?.total ?? 0} 正常`
                    : "尚未巡检"
                }
              />
              <InfoRow label="配置文件" value={system?.storePath ?? "—"} mono />
            </div>

            {system && !system.isRoot && system.useSudo && !system.sudoReady ? (
              <div className="mt-4 rounded-xl border border-amber-400/20 bg-amber-400/[0.06] px-3.5 py-3 text-[11px] leading-relaxed text-amber-200">
                <p className="flex items-center gap-1.5 font-medium">
                  <IconAlert className="h-3.5 w-3.5" />
                  后台自动修复可能因权限失败
                </p>
                <p className="mt-1 text-amber-200/80">
                  请使用 <code className="rounded bg-black/30 px-1 font-mono">sudo</code> 启动后端，或为 route 命令配置免密 sudo。
                  详细步骤见项目根目录 README。
                </p>
                {system.sudoMessage ? (
                  <p className="mt-1.5 font-mono text-[10.5px] text-amber-200/60">{system.sudoMessage}</p>
                ) : null}
              </div>
            ) : null}
          </section>
        </div>

        <div className="mt-6">
          <section className="rounded-2xl border border-white/[0.07] bg-white/[0.025]">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-white/[0.06] px-5 py-4 sm:px-6">
              <div>
                <h2 className="text-base font-semibold text-white">托管路由</h2>
                <p className="mt-1 text-xs text-zinc-500">
                  {summary.total} 条 · 最近核验 {data ? formatClock(data.checkedAt) : "—"}
                  {data?.lastReconcileAt
                    ? ` · 上次巡检 ${formatRelative(data.lastReconcileAt, lastRefresh?.getTime() ?? 0)}`
                    : ""}
                </p>
              </div>
              {summary.total > 0 ? (
                <span
                  className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[11px] font-medium ${
                    abnormal === 0
                      ? "border-emerald-400/25 bg-emerald-400/10 text-emerald-300"
                      : "border-amber-400/25 bg-amber-400/10 text-amber-300"
                  }`}
                >
                  <span className={`h-1.5 w-1.5 rounded-full ${abnormal === 0 ? "bg-emerald-400" : "bg-amber-400"}`} />
                  {abnormal === 0 ? "全部正常" : `${abnormal} 条待处理`}
                </span>
              ) : null}
            </div>

            {summary.total === 0 ? (
              <div className="flex flex-col items-center gap-3 px-6 py-14 text-center">
                <div className="grid h-12 w-12 place-items-center rounded-2xl border border-dashed border-white/15 bg-white/[0.02] text-zinc-500">
                  <IconRoute className="h-5 w-5" />
                </div>
                <div>
                  <p className="text-sm font-medium text-zinc-300">还没有托管路由</p>
                  <p className="mt-1 text-xs text-zinc-500">
                    在上方填写目标地址与网关，例如 5.5.2.13 → 192.168.100.254。
                  </p>
                </div>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full min-w-[760px] text-left">
                  <thead>
                    <tr className="border-b border-white/[0.06] text-[10px] uppercase tracking-[0.14em] text-zinc-500">
                      <th className="px-5 py-3 font-medium sm:px-6">目标地址</th>
                      <th className="px-4 py-3 font-medium">期望网关</th>
                      <th className="px-4 py-3 font-medium">当前走向</th>
                      <th className="px-4 py-3 font-medium">状态</th>
                      <th className="px-5 py-3 text-right font-medium sm:px-6">操作</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/[0.045]">
                    {data?.routes.map((route) => {
                      const meta = STATUS_META[route.state.status] ?? STATUS_META.error;
                      const gatewayMatches = route.state.gateway === route.gateway;
                      const applying = busy === `apply:${route.host}`;
                      return (
                        <tr key={route.host} className="group transition hover:bg-white/[0.02]">
                          <td className="px-5 py-4 sm:px-6">
                            <div className="flex items-center gap-2">
                              <span className="font-mono text-sm font-medium text-white">{route.host}</span>
                              {route.lastError ? (
                                <span
                                  className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-rose-400/10 text-rose-300"
                                  title={route.lastError}
                                >
                                  <IconAlert className="h-2.5 w-2.5" />
                                </span>
                              ) : null}
                            </div>
                            <div className="mt-1 text-[11px] text-zinc-600">
                              更新于 {formatClock(route.updatedAt)}
                            </div>
                          </td>
                          <td className="px-4 py-4 font-mono text-sm text-zinc-300">{route.gateway}</td>
                          <td className="px-4 py-4">
                            <div className={`font-mono text-sm ${route.state.gateway ? (gatewayMatches ? "text-emerald-300" : "text-amber-300") : "text-zinc-600"}`}>
                              {route.state.gateway ?? "—"}
                            </div>
                            <div className="mt-1 flex items-center gap-1.5 text-[11px] text-zinc-600">
                              <span>{route.state.interface ?? "无接口"}</span>
                              {route.state.flags.length > 0 ? (
                                <>
                                  <span>·</span>
                                  <span className="font-mono">{route.state.flags.join(",")}</span>
                                </>
                              ) : null}
                            </div>
                          </td>
                          <td className="px-4 py-4">
                            <span
                              className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-medium ${meta.badge}`}
                              title={`${route.state.message}\nflags: ${route.state.flags.join(",") || "-"}`}
                            >
                              <span className={`h-1.5 w-1.5 rounded-full ${meta.dot}`} />
                              {meta.label}
                            </span>
                            <div
                              className="mt-1 max-w-[230px] truncate text-[11px] text-zinc-500"
                              title={route.state.message}
                            >
                              {route.state.message}
                            </div>
                          </td>
                          <td className="px-5 py-4 sm:px-6">
                            <div className="flex items-center justify-end gap-1.5">
                              <button
                                type="button"
                                onClick={() => void handleApply(route)}
                                disabled={busy !== null}
                                className={GHOST_BUTTON}
                                title="立即删除并按期望重建该主机路由"
                              >
                                {applying ? <Spinner className="h-3.5 w-3.5" /> : <IconWrench className="h-3.5 w-3.5" />}
                                修复
                              </button>
                              <button
                                type="button"
                                onClick={() => setDeleteTarget(route)}
                                disabled={busy !== null}
                                className="inline-flex items-center justify-center rounded-lg border border-white/10 bg-white/[0.03] p-2 text-zinc-400 transition hover:border-rose-400/30 hover:bg-rose-400/10 hover:text-rose-300 disabled:cursor-not-allowed disabled:opacity-40"
                                title="停止托管并删除主机路由"
                                aria-label={`删除 ${route.host}`}
                              >
                                <IconTrash className="h-3.5 w-3.5" />
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
        </div>

        <div className="mt-6 grid items-start gap-6 xl:grid-cols-[minmax(0,1.72fr)_minmax(340px,1fr)]">
          <section className="rounded-2xl border border-white/[0.07] bg-white/[0.025]">
            <button
              type="button"
              onClick={() => setTableOpen((open) => !open)}
              className="flex w-full items-center justify-between gap-3 px-5 py-4 text-left sm:px-6"
              aria-expanded={tableOpen}
            >
              <div className="flex items-center gap-2.5">
                <IconTerminal className="h-4 w-4 text-zinc-500" />
                <div>
                  <h2 className="text-sm font-semibold text-white">内核路由表快照</h2>
                  <p className="mt-0.5 font-mono text-[11px] text-zinc-500">
                    {table?.command ?? system?.commands?.table ?? "netstat -rn"}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                {tableOpen ? (
                  <span className="hidden text-[11px] text-zinc-600 sm:inline">
                    每 {TABLE_POLL_MS / 1000} 秒刷新
                  </span>
                ) : null}
                <IconChevron className={`h-4 w-4 text-zinc-500 transition ${tableOpen ? "rotate-180" : ""}`} />
              </div>
            </button>

            {tableOpen ? (
              <div className="border-t border-white/[0.06] px-5 pb-5 pt-4 sm:px-6">
                <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                  <span className="text-[11px] text-zinc-500">
                    {table ? `${table.entries.length} 条 IPv4 记录` : "加载中…"}
                    {table ? ` · 抓取于 ${formatClock(table.fetchedAt)}` : ""}
                  </span>
                  <div className="flex items-center gap-1.5">
                    <button type="button" onClick={() => void loadTable()} disabled={tableLoading} className={GHOST_BUTTON}>
                      {tableLoading ? <Spinner className="h-3.5 w-3.5" /> : <IconRefresh className="h-3.5 w-3.5" />}
                      刷新
                    </button>
                    <button type="button" onClick={() => void copyTable()} disabled={!table?.raw} className={GHOST_BUTTON}>
                      <IconCopy className="h-3.5 w-3.5" />
                      复制
                    </button>
                  </div>
                </div>

                {table?.error ? (
                  <p className="mb-3 rounded-lg border border-amber-400/20 bg-amber-400/[0.06] px-3 py-2 text-xs text-amber-200">
                    读取路由表失败：{table.error}
                  </p>
                ) : null}

                <pre className="max-h-[380px] overflow-auto rounded-xl border border-white/[0.06] bg-black/40 p-4 font-mono text-[11.5px] leading-relaxed text-zinc-300">
                  {table?.raw || "暂无数据"}
                </pre>
                <p className="mt-2 text-[11px] text-zinc-600">
                  托管路由在表中会以 HOST 主机路由出现；若仅命中 default/网段路由，状态会显示为缺失。
                </p>
              </div>
            ) : null}
          </section>

          <section className="rounded-2xl border border-white/[0.07] bg-white/[0.025]">
            <div className="flex items-center justify-between gap-3 border-b border-white/[0.06] px-5 py-4">
              <div>
                <h2 className="text-base font-semibold text-white">事件日志</h2>
                <p className="mt-1 text-[11px] text-zinc-500">最近 80 条后端操作与巡检记录</p>
              </div>
              <span className="inline-flex items-center gap-1.5 rounded-full border border-white/10 bg-white/[0.03] px-2 py-0.5 text-[10px] text-zinc-400">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-400" />
                LIVE
              </span>
            </div>
            <div className="max-h-[480px] overflow-y-auto">
              {logs.length === 0 ? (
                <p className="px-5 py-10 text-center text-xs text-zinc-500">暂无事件，执行一次巡检试试。</p>
              ) : (
                <ul className="divide-y divide-white/[0.045]">
                  {logs.map((event) => {
                    const level = LEVEL_META[event.level] ?? LEVEL_META.info;
                    return (
                      <li key={event.id} className="flex gap-3 px-5 py-3">
                        <span className="w-14 shrink-0 pt-0.5 font-mono text-[11px] tabular-nums text-zinc-600">
                          {formatClock(event.at)}
                        </span>
                        <span className={`mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full ${level.dot}`} />
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-1.5 text-[11px]">
                            <span className={`font-medium ${level.text}`}>{level.label}</span>
                            <span className="text-zinc-600">{ACTION_LABEL[event.action] ?? event.action}</span>
                            {event.host ? <span className="font-mono text-zinc-300">{event.host}</span> : null}
                          </div>
                          <p className="mt-0.5 break-words text-[12px] leading-relaxed text-zinc-400">
                            {event.message}
                          </p>
                          {event.command ? (
                            <p
                              className="mt-1 truncate font-mono text-[10.5px] text-zinc-600"
                              title={event.command}
                            >
                              $ {event.command}
                            </p>
                          ) : null}
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
          </section>
        </div>

        <footer className="mt-10 flex flex-col items-center justify-between gap-2 border-t border-white/[0.05] pt-5 text-[11px] text-zinc-600 sm:flex-row">
          <p>
            route-daemon v{system?.version ?? "1.0.0"} · 命令以 argv 方式执行，不经过 shell
          </p>
          <p className="font-mono">netstat -rn · route -n get · sudo route add/delete -host</p>
        </footer>
      </main>

      {deleteTarget ? (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-5 backdrop-blur-sm"
          role="dialog"
          aria-modal="true"
          aria-labelledby="delete-title"
          onClick={() => (busy === "delete" ? null : setDeleteTarget(null))}
        >
          <div
            className="w-full max-w-md rounded-2xl border border-white/10 bg-[#0f1116] p-6 shadow-2xl"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="flex items-start gap-3">
              <div className="grid h-10 w-10 shrink-0 place-items-center rounded-xl border border-rose-400/25 bg-rose-400/10 text-rose-300">
                <IconTrash className="h-5 w-5" />
              </div>
              <div>
                <h3 id="delete-title" className="text-base font-semibold text-white">
                  停止托管并删除路由？
                </h3>
                <p className="mt-1 text-sm leading-relaxed text-zinc-400">
                  将从托管列表移除{" "}
                  <span className="font-mono text-zinc-200">{deleteTarget.host}</span>，并删除内核中的主机路由
                  <span className="font-mono text-zinc-300"> → {deleteTarget.gateway}</span>。守护进程不会再自动恢复它。
                </p>
              </div>
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setDeleteTarget(null)}
                disabled={busy === "delete"}
                className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-2 text-sm text-zinc-300 transition hover:bg-white/[0.07] disabled:opacity-50"
              >
                取消
              </button>
              <button
                type="button"
                onClick={() => void handleDelete()}
                disabled={busy === "delete"}
                className="inline-flex items-center gap-2 rounded-xl bg-rose-500 px-4 py-2 text-sm font-semibold text-white transition hover:bg-rose-400 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {busy === "delete" ? <Spinner className="h-4 w-4" /> : <IconTrash className="h-4 w-4" />}
                确认删除
              </button>
            </div>
          </div>
        </div>
      ) : null}

      {toast ? <Toast toast={toast} onClose={() => setToast(null)} /> : null}
    </div>
  );
}

function StatCard({
  label,
  value,
  hint,
  icon,
  tone,
}: {
  label: string;
  value: string;
  hint: string;
  icon: ReactNode;
  tone: "zinc" | "cyan" | "emerald" | "amber";
}) {
  const tones = {
    zinc: "text-zinc-300 border-white/10 bg-white/[0.03]",
    cyan: "text-cyan-300 border-cyan-400/20 bg-cyan-400/10",
    emerald: "text-emerald-300 border-emerald-400/20 bg-emerald-400/10",
    amber: "text-amber-300 border-amber-400/20 bg-amber-400/10",
  } as const;
  return (
    <div className="rounded-2xl border border-white/[0.07] bg-white/[0.025] p-4 sm:p-5">
      <div className="flex items-center justify-between">
        <span className="text-[11px] font-medium uppercase tracking-[0.14em] text-zinc-500">{label}</span>
        <span className={`grid h-8 w-8 place-items-center rounded-lg border ${tones[tone]}`}>{icon}</span>
      </div>
      <p className="mt-3 text-2xl font-semibold tracking-tight text-white">{value}</p>
      <p className="mt-1 truncate text-[11px] text-zinc-500" title={hint}>
        {hint}
      </p>
    </div>
  );
}

function InfoRow({
  label,
  value,
  mono = false,
  tone,
}: {
  label: string;
  value: string;
  mono?: boolean;
  tone?: "emerald" | "amber";
}) {
  const toneClass = tone === "emerald" ? "text-emerald-300" : tone === "amber" ? "text-amber-300" : "text-zinc-300";
  return (
    <div className="flex items-start justify-between gap-4">
      <span className="shrink-0 text-zinc-500">{label}</span>
      <span className={`min-w-0 truncate text-right ${mono ? "font-mono" : ""} ${toneClass}`} title={value}>
        {value}
      </span>
    </div>
  );
}

function Switch({
  checked,
  onChange,
  disabled,
  label,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  label: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full border transition ${
        checked ? "border-cyan-300/40 bg-cyan-400" : "border-white/10 bg-zinc-700"
      } disabled:cursor-not-allowed disabled:opacity-50`}
    >
      <span
        className={`absolute top-0.5 h-[18px] w-[18px] rounded-full bg-white shadow transition-transform ${
          checked ? "translate-x-[22px]" : "translate-x-1"
        }`}
      />
    </button>
  );
}

function Toast({ toast, onClose }: { toast: ToastState; onClose: () => void }) {
  const styles = {
    success: "border-emerald-400/25 bg-[#0c1712] text-emerald-200",
    error: "border-rose-400/25 bg-[#1a0e11] text-rose-200",
    info: "border-sky-400/25 bg-[#0b131b] text-sky-200",
  } as const;
  return (
    <div className="fixed bottom-5 left-1/2 z-[60] w-[calc(100%-2rem)] max-w-xl -translate-x-1/2">
      <div className={`flex items-start gap-3 rounded-2xl border px-4 py-3 shadow-2xl backdrop-blur ${styles[toast.kind]}`}>
        <span className="mt-0.5 shrink-0">
          {toast.kind === "success" ? (
            <IconCheck className="h-4 w-4" />
          ) : toast.kind === "error" ? (
            <IconAlert className="h-4 w-4" />
          ) : (
            <IconInfo className="h-4 w-4" />
          )}
        </span>
        <p className="flex-1 text-sm leading-relaxed">{toast.text}</p>
        <button
          type="button"
          onClick={onClose}
          className="mt-0.5 shrink-0 rounded p-0.5 opacity-60 transition hover:opacity-100"
          aria-label="关闭提示"
        >
          <IconClose className="h-3.5 w-3.5" />
        </button>
      </div>
    </div>
  );
}

function Spinner({ className = "h-4 w-4" }: { className?: string }) {
  return (
    <svg className={`animate-spin ${className}`} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" className="opacity-20" />
      <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  );
}

function formatClock(value?: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleTimeString("zh-CN", { hour12: false });
}

function formatRelative(value: string | null | undefined, nowMs: number): string {
  if (!value || !nowMs) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  const seconds = Math.max(0, Math.floor((nowMs - date.getTime()) / 1000));
  if (seconds < 5) return "刚刚";
  if (seconds < 60) return `${seconds} 秒前`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`;
  return `${Math.floor(seconds / 86400)} 天前`;
}

type IconProps = { className?: string };

function IconRoute({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
      <circle cx="6" cy="18" r="2.25" />
      <circle cx="18" cy="6" r="2.25" />
      <path strokeLinecap="round" strokeLinejoin="round" d="M8.25 18h3.5a3 3 0 0 0 3-3V9.75a3 3 0 0 1 3-3h.25" />
      <path strokeLinecap="round" d="M13 15h-2.5" />
    </svg>
  );
}

function IconShield({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M9 12.75 11.25 15 15 9.75M21 12c0 1.268-.63 2.39-1.593 3.068a3.745 3.745 0 0 1-1.043 3.296 3.745 3.745 0 0 1-3.296 1.043A3.745 3.745 0 0 1 12 21c-1.268 0-2.39-.63-3.068-1.593a3.746 3.746 0 0 1-3.296-1.043 3.745 3.745 0 0 1-1.043-3.296A3.745 3.745 0 0 1 3 12c0-1.268.63-2.39 1.593-3.068a3.745 3.745 0 0 1 1.043-3.296 3.746 3.746 0 0 1 3.296-1.043A3.746 3.746 0 0 1 12 3c1.268 0 2.39.63 3.068 1.593a3.746 3.746 0 0 1 3.296 1.043 3.746 3.746 0 0 1 1.043 3.296A3.745 3.745 0 0 1 21 12Z" />
    </svg>
  );
}

function IconBolt({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 13.5 14.25 2.25 12 10.5h8.25L9.75 21.75 12 13.5H3.75Z" />
    </svg>
  );
}

function IconServer({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M21 7.5 12 2.25 3 7.5m18 0-9 5.25m9-5.25v9l-9 5.25M3 7.5l9 5.25M3 7.5v9l9 5.25m0-9v9" />
    </svg>
  );
}

function IconRefresh({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M16.023 9.348h4.992V4.356m0 4.992-3.181-3.183a8.25 8.25 0 0 0-13.803 3.7M2.985 19.644v-4.992m0 0h4.992m-4.993 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7" />
    </svg>
  );
}

function IconWrench({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M11.42 15.17 17.25 21A2.652 2.652 0 0 0 21 17.25l-5.877-5.877M11.42 15.17l2.496-3.03c.317-.384.74-.626 1.208-.766M11.42 15.17l-4.655 5.653a2.548 2.548 0 1 1-3.586-3.586l6.837-5.63m5.108-.233c.55-.164 1.163-.188 1.743-.14a4.5 4.5 0 0 0 4.486-6.336l-3.276 3.277a3.004 3.004 0 0 1-2.25-2.25l3.276-3.276a4.5 4.5 0 0 0-6.336 4.486c.091 1.076-.071 2.264-.904 2.95l-.102.085m-1.745 1.437L5.909 7.5H4.5L2.25 3.75l1.5-1.5L7.5 4.5v1.409l4.26 4.26m-1.745 1.437 1.745-1.437m6.615 8.206L15.75 15.75M4.867 19.125h.008v.008h-.008v-.008Z" />
    </svg>
  );
}

function IconTrash({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" />
    </svg>
  );
}

function IconPlus({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M12 4.5v15m7.5-7.5h-15" />
    </svg>
  );
}

function IconTerminal({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M6.75 7.5 9.75 9.75 6.75 12m4.5 0h3m-9.75 7.5h15a2.25 2.25 0 0 0 2.25-2.25V6.75A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25v10.5a2.25 2.25 0 0 0 2.25 2.25Z" />
    </svg>
  );
}

function IconCopy({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 0 1-1.125-1.125V7.875c0-.621.504-1.125 1.125-1.125H6.75a9.06 9.06 0 0 1 1.5.124m7.5 10.376h3.375c.621 0 1.125-.504 1.125-1.125V11.25c0-4.46-3.243-8.161-7.5-8.876a9.06 9.06 0 0 0-1.5-.124H9.375c-.621 0-1.125.504-1.125 1.125v3.5m7.5 10.375H9.375a1.125 1.125 0 0 1-1.125-1.125v-9.25m12 6.625v-1.875a3.375 3.375 0 0 0-3.375-3.375h-1.5a1.125 1.125 0 0 1-1.125-1.125v-1.5a3.375 3.375 0 0 0-3.375-3.375H9.75" />
    </svg>
  );
}

function IconChevron({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="m19.5 8.25-7.5 7.5-7.5-7.5" />
    </svg>
  );
}

function IconAlert({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z" />
    </svg>
  );
}

function IconCheck({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="m4.5 12.75 6 6 9-13.5" />
    </svg>
  );
}

function IconInfo({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="m11.25 11.25.041-.02a.75.75 0 0 1 1.063.852l-.708 2.836a.75.75 0 0 0 1.063.853l.041-.021M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9-3.75h.008v.008H12V8.25Z" />
    </svg>
  );
}

function IconClose({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M6 18 18 6M6 6l12 12" />
    </svg>
  );
}
