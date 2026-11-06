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
	"syscall"
	"time"

	"be/internal/healer"
	"be/internal/httpapi"
	"be/internal/store"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP 监听地址")
	storePath := flag.String("store", "./data/routes.json", "托管路由持久化文件")
	interval := flag.Int("interval", 5, "巡检间隔（秒）")
	flag.Parse()

	if *interval < 1 {
		*interval = 5
	}

	st, err := store.New(*storePath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	h := healer.New(st, time.Duration(*interval)*time.Second)

	log.Printf("route-daemon (%s/%s)", runtime.GOOS, runtime.GOARCH)
	log.Printf("监听: http://%s", *listen)
	log.Printf("配置: %s（%d 条托管路由）", *storePath, len(st.Routes()))
	log.Printf("巡检间隔: %d 秒", *interval)
	if os.Geteuid() != 0 {
		log.Printf("警告: 当前不是 root，修改路由表会失败，请使用 sudo 运行")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go h.Run(ctx)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           httpapi.New(h),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务启动失败: %v", err)
		}
	}()
	fmt.Println("按 Ctrl+C 退出")

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
