// musicd 是「茶与金平糖」的后端入口：单二进制，无外部依赖。
//
// 启动流程（docs/architecture.md §6/§9）：
//  1. 加载配置（默认值 → config.example.json → config.local.json → env）
//  2. 建 provider 注册表（demo 始终注册；有 cookie / 指定 wy 时注册 netease）
//  3. 启动 room hub（内置电台房保证存在）与 500ms 巡检循环
//  4. 启动 HTTP 服务（API + WS + 静态资源），等 SIGINT/SIGTERM 优雅退出
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hakrelin/music/server/internal/config"
	"github.com/hakrelin/music/server/internal/httpapi"
	"github.com/hakrelin/music/server/internal/hub"
	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider"
	"github.com/hakrelin/music/server/internal/provider/demo"
	"github.com/hakrelin/music/server/internal/provider/netease"
	"github.com/hakrelin/music/server/internal/storage"
)

// version 可通过构建参数覆盖：-ldflags "-X main.version=1.2.3"。
var version = "1.0.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "musicd 启动失败:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	if cfg.DataDir != "" {
		if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
			// 数据目录 v1 还没用上，创建失败只警告，不阻止启动。
			logger.Warn("数据目录创建失败", "dir", cfg.DataDir, "err", err)
		}
	}

	// 1) provider 注册表：demo 永远在，保证任何配置下都能开箱即用。
	reg := provider.NewRegistry()
	demoProvider := demo.New()
	reg.Register(demoProvider)

	primary := "demo"
	useWY := strings.EqualFold(cfg.Provider, "wy") ||
		(strings.EqualFold(cfg.Provider, "auto") && strings.TrimSpace(cfg.NeteaseCookie) != "")
	if useWY {
		wy := netease.New(netease.Config{Cookie: cfg.NeteaseCookie})
		reg.Register(wy)
		primary = "wy"
	}
	logger.Info("provider 就绪", "primary", primary, "sources", reg.Names())

	// 1.5) 音频缓存：多吉云对象存储。听过的歌落桶，下次播放 302 过去直连 COS。
	cache, cacheResolve := buildCache(cfg, reg, logger)

	// 2) 房间 hub
	idle := time.Duration(cfg.RoomIdleMinutes) * time.Minute
	var onPlayed func(model.Track)
	if cache.Enabled() {
		onPlayed = func(t model.Track) { cache.Ensure(t.Source, t.ID, cacheResolve) }
	}
	h := hub.New(hub.Options{
		Version:         version,
		Registry:        reg,
		PrimaryProvider: primary,
		RootPassword:    cfg.RootPassword,
		AdminPassword:   cfg.AdminPassword,
		VoteSkipRatio:   cfg.VoteSkipRatio,
		IdleTimeout:     idle,
		RadioRooms:      cfg.RadioRooms,
		Logger:          logger,
		OnTrackPlayed:   onPlayed,
	})
	h.StartBuiltinRooms()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go h.Run(ctx)
	cache.Run(ctx)

	// 3) HTTP
	api := httpapi.New(httpapi.Options{
		Config:       cfg,
		Hub:          h,
		Registry:     reg,
		Demo:         demoProvider,
		Cache:        cache,
		CacheResolve: cacheResolve,
		Version:      version,
		Logger:       logger,
	})
	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: api.Handler(),
		// 只给普通 HTTP 请求设超时；WebSocket 是 hijack 的，不受这些限制。
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", cfg.HTTPAddr, err)
	}
	logger.Info("musicd 已启动",
		"addr", ln.Addr().String(),
		"provider", primary,
		"rooms", h.RoomCount(),
		"idleReapMinutes", cfg.RoomIdleMinutes,
	)

	// 优雅退出：先停 HTTP（拒绝新连接），再让 hub 断开所有 WS。
	go func() {
		<-ctx.Done()
		logger.Info("收到退出信号，正在关闭……")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP 服务异常退出: %w", err)
	}
	logger.Info("musicd 已退出")
	return nil
}

// buildCache 构造音频缓存与「解析 128k 地址」回调。
//
// 为什么缓存用 128k：多吉云「简单上传」接口上限 10MB，320k 的 5 分钟歌就有
// 18MB 压不进去；128k 一般 4~7MB，能稳定落桶。直连播放仍然是 320k。
func buildCache(cfg config.Config, reg *provider.Registry, logger *slog.Logger) (*storage.Cache, storage.Resolver) {
	resolve := func(ctx context.Context, source, id string) (string, error) {
		p, ok := reg.For(source)
		if !ok {
			return "", fmt.Errorf("未知音乐源 %q", source)
		}
		t := model.Track{ID: id, Source: source}
		if rp, ok := p.(provider.RefreshProvider); ok {
			if err := rp.ResolveFresh(ctx, &t, true); err != nil {
				return "", err
			}
		} else if err := p.Resolve(ctx, &t); err != nil {
			return "", err
		}
		return t.URL, nil
	}
	if !cfg.Storage.Enabled {
		return nil, resolve
	}
	accs := make([]storage.Account, 0, len(cfg.Storage.Accounts))
	for _, a := range cfg.Storage.Accounts {
		accs = append(accs, storage.Account{Label: a.Label, AK: a.AccessKey, SK: a.SecretKey,
			Bucket: a.Bucket, Base: a.PublicBase})
	}
	cache := storage.New(storage.Options{
		Accounts:           accs,
		DataDir:            cfg.DataDir,
		MaxBytesPerAccount: int64(cfg.Storage.MaxMBPerAccount) << 20,
		EvictAfterDays:     cfg.Storage.EvictAfterDays,
		MinHits:            cfg.Storage.MinHits,
		Download:           netease.DownloadAudio,
		Logger:             logger,
	})
	if cache.Enabled() {
		logger.Info("音频缓存已启用", "buckets", len(accs), "dataDir", cfg.DataDir,
			"maxMBPerAccount", cfg.Storage.MaxMBPerAccount, "evictAfterDays", cfg.Storage.EvictAfterDays)
	} else {
		logger.Warn("音频缓存未启用（没有有效的存储账号）")
	}
	return cache, resolve
}

// newLogger 按配置的 logLevel 构造 slog logger。
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
