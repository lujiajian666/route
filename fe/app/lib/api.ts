export type RouteStatus = "ok" | "missing" | "gateway_mismatch" | "error";

export type RouteView = {
  host: string;
  gateway: string;
  createdAt: string;
  status: RouteStatus;
  present: boolean;
  hostRoute: boolean;
  currentGateway?: string;
  interface?: string;
  flags: string[];
  message: string;
  checkedAt: string;
};

export type Summary = {
  total: number;
  ok: number;
  missing: number;
  gatewayMismatch: number;
  error: number;
};

export type RoutesResponse = {
  routes: RouteView[];
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

export type AddRouteResponse = {
  route: RouteView;
  created: boolean;
};

export type ApplyRouteResponse = {
  route: RouteView;
};

export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

const API_BASE = (process.env.NEXT_PUBLIC_API_BASE ?? "").replace(/\/+$/, "");

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`${API_BASE}/api${path}`, {
      ...init,
      cache: "no-store",
      headers: {
        "Content-Type": "application/json",
        ...(init?.headers ?? {}),
      },
    });
  } catch {
    throw new ApiError("无法连接后端服务", 0);
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
    throw new ApiError(message, response.status);
  }
  return payload as T;
}

export function fetchRoutes(): Promise<RoutesResponse> {
  return request<RoutesResponse>("/routes");
}

export function fetchRouteTable(): Promise<RouteTableResponse> {
  return request<RouteTableResponse>("/route-table");
}

export function addRoute(host: string, gateway: string): Promise<AddRouteResponse> {
  return request<AddRouteResponse>("/routes", {
    method: "POST",
    body: JSON.stringify({ host, gateway }),
  });
}

export function applyRoute(host: string): Promise<ApplyRouteResponse> {
  return request<ApplyRouteResponse>(`/routes/${encodeURIComponent(host)}/apply`, {
    method: "POST",
  });
}

export function deleteRoute(host: string): Promise<{ removed: boolean }> {
  return request<{ removed: boolean }>(`/routes/${encodeURIComponent(host)}`, {
    method: "DELETE",
  });
}

export function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  return "未知错误";
}
