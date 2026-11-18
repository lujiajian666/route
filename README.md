# RouteKeeper

Go + Gin 后端，Next.js 前端。用来管理 macOS / Linux 的主机路由和网段路由：添加、删除、定时巡检，路由被系统重置后自动重新添加，并用 `route -n get` 和 `ipconfig getpacket` 核验网关。

## 运行

```bash
make
```

会同时启动：

- 后端：`http://127.0.0.1:8080`（通过 `sudo` 启动，需要输入密码）
- 前端：`http://127.0.0.1:3000`

也可以分开启动：

```bash
make backend    # cd be && go build -o route-daemon . && sudo ./route-daemon
make frontend   # cd fe && npm run dev
```

> 修改路由表需要 root 权限，所以后端必须用 `sudo` 运行。前端仍然用普通用户运行。

## 使用

1. 打开 http://127.0.0.1:3000
2. 填写目标地址和网关，例如 `5.5.2.13` 或 `5.5.2.0/24` / `192.168.100.254`，点「添加」
   - 单个 IP 会添加主机路由（`route add -host`）
   - CIDR 会添加网段路由（`route add -net`），`5.5.2.11/24` 会自动规范成 `5.5.2.0/24`
3. 列表会显示当前实际网关、接口和状态，异常时后端会自动重建
4. 页面下方的「路由表」直接展示 `netstat -rn`

状态包括：正常、缺失、网关不符、失败。

后端每 5 秒检查一次所有托管路由。检查分两步：

1. `route -n get <目标>` 确认存在对应的主机/网段路由，且网关等于配置的网关；
2. `ipconfig getpacket <接口>` 读取该接口的 DHCP 网关，确认和配置的网关一致。

任意一步不对，就删除后重新添加：

```bash
# 主机路由
route delete -host <目标>
route add -host <目标> <网关>

# 网段路由
route delete -net <CIDR>
route add -net <CIDR> <网关>

route -n get <目标>
```

确认恢复成功后记录日志。托管列表保存在 `be/data/routes.json`，重启后仍然有效。

## 目录

```text
route/
├── Makefile
├── be/                    # Go + Gin 后端
│   ├── main.go
│   └── internal/
│       ├── httpapi/       # API
│       ├── healer/        # 巡检 / 自动恢复
│       ├── routecmd/      # route / ip 命令与输出解析
│       ├── store/         # JSON 持久化
│       ├── execx/         # 命令执行
│       ├── validate/      # IPv4 校验
│       └── model/
├── fe/                    # Next.js 16 前端
│   ├── app/page.tsx
│   ├── app/components/route-dashboard.tsx
│   ├── app/lib/api.ts
│   └── next.config.ts     # /api/* 代理到 8080
└── README.md
```

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/routes` | 托管路由及实时状态 |
| POST | `/api/routes` | 添加路由：`{"host":"5.5.2.13","gateway":"192.168.100.254"}`，host 也支持 CIDR |
| DELETE | `/api/routes?target=<目标或CIDR>` | 删除路由并停止托管 |
| GET | `/api/route-table` | `netstat -rn` 原始输出 |
| GET | `/api/health` | 健康检查 |

示例：

```bash
curl -s http://127.0.0.1:8080/api/routes
curl -s -X POST http://127.0.0.1:8080/api/routes \
  -H 'Content-Type: application/json' \
  -d '{"host":"5.5.2.13","gateway":"192.168.100.254"}'

curl -s -X POST http://127.0.0.1:8080/api/routes \
  -H 'Content-Type: application/json' \
  -d '{"host":"5.5.2.0/24","gateway":"192.168.100.254"}'

curl -s -X DELETE 'http://127.0.0.1:8080/api/routes?target=5.5.2.0%2F24'
```
