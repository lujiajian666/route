package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"be/internal/events"
	"be/internal/execx"
	"be/internal/healer"
	"be/internal/httpapi"
	"be/internal/model"
	"be/internal/routecmd"
	"be/internal/store"
)

const version = "1.0.0"

func main() {
	var (
		listen      = flag.String("listen", envString("ROUTE_LISTEN", "127.0.0.1:8080"), "HTTP 监听地址")
		storePath   = flag.String("store", envString("ROUTE_STORE", "./data/routes.json"), "托管路由持久化文件")
		dryRun      = flag.Bool("dry-run", envBool("ROUTE_DRY_RUN", false), "只打印/模拟命令，不真正修改路由表")
		useSudo     = flag.Bool("sudo", envBool("ROUTE_USE_SUDO", os.Geteuid() != 0), "使用 sudo -n 执行 route 命令")
		autoHeal    = flag.Bool("auto-heal", envBool("ROUTE_AUTO_HEAL", true), "启动时默认开启自动修复（仅首次创建配置文件时生效）")
		interval    = flag.Int("interval", envInt("ROUTE_INTERVAL", 5), "启动时默认巡检间隔秒数（仅首次创建配置文件时生效）")
		origins     = flag.String("cors-origin", envString("ROUTE_CORS_ORIGIN", "http://localhost:3000,http://127.0.0.1:3000"), "允许跨域的来源，逗号分隔，* 表示全部")
		showVersion = flag.Bool("version", false, "打印版本后退出")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "route-daemon %s - 主机路由守护服务\n\n", version)
		fmt.Fprintf(flag.CommandLine.Output(), "用法: sudo %s [flags]\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *interval < 1 {
		*interval = 1
	}

	defaults := model.Settings{AutoHeal: *autoHeal, IntervalSeconds: *interval}
	st, err := store.New(*storePath, defaults)
	if err != nil {
		log.Fatalf("初始化配置失败: %v", err)
	}

	runner := &execx.Runner{
		Sudo:    *useSudo && os.Geteuid() != 0,
		SudoBin: "sudo",
		DryRun:  *dryRun,
		Timeout: 10 * time.Second,
	}
	ops := routecmd.New(runner)
	logStore := events.New(500)
	h := healer.New(st, ops, runner, logStore, *dryRun, version)

	cfg := httpapi.Config{
		Version:        version,
		Listen:         *listen,
		StorePath:      *storePath,
		DryRun:         *dryRun,
		AllowedOrigins: splitCSV(*origins),
	}
	engine := httpapi.New(h, cfg)

	printBanner(*listen, *storePath, runner, ops, st.Settings(), *dryRun)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go h.Run(ctx)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           engine,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("HTTP 服务已启动：http://%s", displayAddr(*listen))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Println("收到退出信号，正在关闭…")
	case err := <-errCh:
		log.Fatalf("HTTP 服务启动失败: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("关闭 HTTP 服务出错: %v", err)
	}
}

func printBanner(listen, storePath string, runner *execx.Runner, ops *routecmd.Ops, settings model.Settings, dryRun bool) {
	log.Printf("route-daemon %s (%s/%s)", version, runtime.GOOS, runtime.GOARCH)
	log.Printf("监听地址: http://%s", displayAddr(listen))
	log.Printf("托管配置: %s", storePath)
	log.Printf("自动修复: %s，巡检间隔: %ds", onOff(settings.AutoHeal), settings.IntervalSeconds)
	if dryRun {
		log.Printf("⚠️  dry-run 模式：所有修改路由表的命令只模拟、不执行")
	}
	if os.Geteuid() == 0 {
		log.Printf("权限: 以 root 运行，可直接修改路由表")
		return
	}
	if !runner.Sudo {
		log.Printf("⚠️  未启用 sudo，修改路由表很可能因权限不足失败")
		return
	}
	probeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ok, msg := ops.ProbeSudo(probeCtx)
	if ok {
		log.Printf("权限: sudo -n 可执行路由命令")
		return
	}
	log.Printf("⚠️  sudo 需要密码，后台无法自动修复路由")
	log.Printf("    方案一：使用 root 启动，例如 sudo %s", os.Args[0])
	log.Printf("    方案二：为 route 命令配置免密 sudo（见 README）")
	if msg != "" {
		log.Printf("    sudo 诊断: %s", firstLine(msg))
	}
}

func displayAddr(listen string) string {
	if strings.HasPrefix(listen, ":") {
		return "127.0.0.1" + listen
	}
	return listen
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return s
}

func onOff(v bool) string {
	if v {
		return "开启"
	}
	return "关闭"
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envString(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}
