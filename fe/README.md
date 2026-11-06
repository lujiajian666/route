# RouteKeeper 前端（Next.js 16）

这是 RouteKeeper 的 Web 控制台。完整安装、权限配置与使用说明见上级目录的 [README](../README.md)。

## 开发

```bash
npm install
npm run dev
```

打开 http://localhost:3000 或 http://127.0.0.1:3000。

`/api/*` 会由 `next.config.ts` 代理到 Go 后端（默认 `http://127.0.0.1:8080`）。后端不在默认端口时：

```bash
BACKEND_ORIGIN=http://127.0.0.1:9090 npm run dev
```

## 生产构建

```bash
BACKEND_ORIGIN=http://127.0.0.1:8080 npm run build
npm run start
```

## 检查

```bash
npx tsc --noEmit
npm run lint
npm run build
```
