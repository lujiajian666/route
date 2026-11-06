export type RouteStatus = "ok" | "missing" | "gateway_mismatch" | "error";

export type RouteState = {
  status: RouteStatus;
  present: boolean;
  hostRoute: boolean;
  gateway?: string;
  interface?: string;
  flags: string[];
  checkedAt: string;
  message: string;
};

export type RouteView = {
  host: string;
  gateway: string;
  createdAt: string;
  updatedAt: string;
  state: RouteState;
  lastAppliedAt?: string;
  lastError?: string;
};

export type Summary = {
  total: number;
  ok: number;
  missing: number;
  gatewayMismatch: number;
  error: number;
};

export type Settings = {
  autoHeal: boolean;
  intervalSeconds: number;
};

export type RoutesResponse = {
  routes: RouteView[];
  summary: Summary;
  settings: Settings;
  checkedAt: string;
  dryRun: boolean;
  lastReconcileAt?: string;
  lastReconcileSummary?: Summary;
  lastReconcileError?: string;
};

export type SystemInfo = {
  version: string;
  platform: string;
  arch: string;
  goVersion: string;
  hostname: string;
  pid: number;
  isRoot: boolean;
  useSudo: boolean;
  sudoReady: boolean;
  sudoMessage?: string;
  dryRun: boolean;
  listen: string;
  storePath: string;
  settings: Settings;
  startedAt: string;
  serverTime: string;
  routeBinary: string;
  commands: Record<"get" | "add" | "delete" | "table", string>;
  lastReconcileAt?: string;
  lastReconcileSummary?: Summary;
  lastReconcileError?: string;
};

export type RouteEvent = {
  id: number;
  at: string;
  level: "info" | "success" | "warn" | "error";
  action: string;
  host?: string;
  message: string;
  command?: string;
};

export type ApplyResult = {
  host: string;
  gateway: string;
  trigger: string;
  applied: boolean;
  changed: boolean;
  simulated: boolean;
  state: RouteState;
  message: string;
  warnings?: string[];
  error?: string;
  at: string;
};

export type DeleteResult = {
  host: string;
  gateway: string;
  deleted: boolean;
  simulated: boolean;
  warnings?: string[];
  message: string;
  at: string;
};

export type AddRouteResponse = {
  route: RouteView;
  apply: ApplyResult;
  created: boolean;
};

export type ApplyRouteResponse = {
  apply: ApplyResult;
  route: RouteView;
};

export type DeleteRouteResponse = {
  result: DeleteResult;
  removed: boolean;
};

export type ReconcileResponse = {
  results: ApplyResult[];
  summary: Summary;
  checkedAt: string;
};

export type RouteTableEntry = {
  destination: string;
  gateway: string;
  flags: string;
  netif: string;
  line: string;
  managed: boolean;
};

export type RouteTableResponse = {
  raw: string;
  command: string;
  entries: RouteTableEntry[];
  fetchedAt: string;
  error?: string;
};

export class ApiError extends Error {
  status: number;
  payload: unknown;

  constructor(message: string, status: number, payload: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.payload = payload;
  }
}

const API_BASE = (process.env.NEXT_PUBLIC_API_BASE ?? "").replace(/\/+$/, "");

/** Build an absolute API URL. In production Next rewrites `/api/*` to the Go backend. */
export function apiUrl(path: string): string {
  return `${API_BASE}/api${path}`;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(apiUrl(path), {
      ...init,
      cache: "no-store",
      headers: {
        "Content-Type": "application/json",
        ...(init?.headers ?? {}),
      },
    });
  } catch {
    throw new ApiError("无法连接后端服务，请确认 route-daemon 已启动", 0, null);
  }

  const text = await response.text();
  let payload: unknown = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = { error: text };
    }
  }

  if (!response.ok) {
    const message =
      typeof payload === "object" && payload !== null && "error" in payload && typeof payload.error === "string"
        ? payload.error
        : `请求失败（HTTP ${response.status}）`;
    throw new ApiError(message, response.status, payload);
  }
  return payload as T;
}

export function fetchRoutes(): Promise<RoutesResponse> {
  return request<RoutesResponse>("/routes");
}

export function fetchSystem(): Promise<SystemInfo> {
  return request<SystemInfo>("/system");
}

export function fetchLogs(limit = 80): Promise<{ logs: RouteEvent[] }> {
  return request<{ logs: RouteEvent[] }>(`/logs?limit=${limit}`);
}

export function fetchRouteTable(): Promise<RouteTableResponse> {
  return request<RouteTableResponse>("/route-table");
}

export function addManagedRoute(host: string, gateway: string): Promise<AddRouteResponse> {
  return request<AddRouteResponse>("/routes", {
    method: "POST",
    body: JSON.stringify({ host, gateway }),
  });
}

export function applyManagedRoute(host: string): Promise<ApplyRouteResponse> {
  return request<ApplyRouteResponse>(`/routes/${encodeURIComponent(host)}/apply`, {
    method: "POST",
  });
}

export function deleteManagedRoute(host: string): Promise<DeleteRouteResponse> {
  return request<DeleteRouteResponse>(`/routes/${encodeURIComponent(host)}`, {
    method: "DELETE",
  });
}

export function reconcileAllRoutes(): Promise<ReconcileResponse> {
  return request<ReconcileResponse>("/reconcile", { method: "POST" });
}

export function updateSettings(patch: Partial<Settings>): Promise<{ settings: Settings }> {
  return request<{ settings: Settings }>("/settings", {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return error.message;
  return "未知错误";
}
