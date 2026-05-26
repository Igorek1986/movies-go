package main

import (
	"context"
	"fmt"
	"movies-api/config"
	"movies-api/db"
	"movies-api/db/postgres"
	"movies-api/db/store"
	"strings"

	"movies-api/internal/api"
	"movies-api/internal/auth"
	"movies-api/internal/bot"
	"movies-api/internal/logbuf"
	"movies-api/internal/tasks"
	"movies-api/movies/tmdb"
	"movies-api/parser"
	"movies-api/version"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/jasonlvhit/gocron"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	log.SetOutput(logbuf.Default)
	logbuf.Default.Init("/app/logs")

	cfg := config.Get()

	httpPort := cfg.HTTPPort
	if httpPort == 0 {
		httpPort = 8888
	}

	fmt.Println("=========== START ===========")

	db.Init()

	// Записываем дефолты только если ключа ещё нет в БД
	{
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		for key, val := range store.SettingDefaults {
			if _, ok := store.GetSetting(ctx, key); !ok {
				store.SetSetting(ctx, key, val)
			}
		}
		cancel()
	}

	mode := "parser"
	{
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if m, ok := store.GetSetting(ctx, "app_mode"); ok && m == "all" {
			mode = "all"
		}
		cancel()
	}

	fmt.Printf("lm_%s, %s, CPU: %d, mode: %s\n", version.Version, runtime.Version(), runtime.NumCPU(), mode)

	if mode == "all" {
		ensureSuperuser(cfg)
	}
	tmdb.Init()

	getDbInfo()

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	// Telegram бот и фоновые задачи
	if mode == "all" {
		if err := bot.Start(appCtx); err != nil {
			log.Printf("Telegram bot error: %v", err)
		} else if rawBaseURL, _ := store.GetSetting(appCtx, "base_url"); rawBaseURL != "" && bot.Enabled() {
			baseURL := strings.TrimRight(rawBaseURL, "/")
			usePolling, _ := store.GetSetting(appCtx, "telegram_use_polling")
			if usePolling != "1" {
				webhookURL := baseURL + "/bot/webhook"
				if err := bot.SetWebhook(webhookURL); err != nil {
					log.Printf("Telegram webhook register error: %v", err)
				} else {
					log.Printf("Telegram webhook registered: %s", webhookURL)
				}
			}
			if err := bot.SetMenuButton(baseURL + "/tg-app"); err != nil {
				log.Printf("Telegram menu button error: %v", err)
			} else {
				log.Printf("Telegram menu button set: %s/tg-app", baseURL)
			}
		}
		tasks.Start(appCtx)
	}

	api.InitCategorySettings()
	parser.OnComplete = api.InvalidateCategoryCache

	// HTTP сервер
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", httpPort),
		Handler:      api.NewRouter(mode),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("HTTP server listening on :%d", httpPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Парсер по расписанию
	go func() {
		scanReleases()
		log.Println("Start parser timer")
		gocron.Every(3).Hours().From(calcTime()).Do(scanReleases)
		<-gocron.Start()
	}()

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	log.Println("Shutting down...")
	appCancel()
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutCancel()
	srv.Shutdown(shutCtx) //nolint:errcheck
	log.Println("Done")
}

func ensureSuperuser(cfg *config.ConfigParser) {
	if cfg.SuperUsername == "" || cfg.SuperPassword == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hash, err := auth.HashPassword(cfg.SuperPassword)
	if err != nil {
		log.Println("ensureSuperuser: hash error:", err)
		return
	}
	if err := store.EnsureSuperuser(ctx, cfg.SuperUsername, hash); err != nil {
		log.Println("ensureSuperuser:", err)
	} else {
		log.Printf("Superuser %q ready", cfg.SuperUsername)
	}
}


func scanReleases() {
	parser.RunAll()
	getDbInfo()
	next := calcTime()
	parser.SetNextRunAt(*next)
	log.Printf("parser: следующий запуск в %s", next.Format("02.01.2006 15:04"))
}

func calcTime() *time.Time {
	hour := time.Now().Hour()
	t := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(),
		0, 0, 0, 0, time.Local)
	for _, h := range []int{2, 5, 8, 11, 14, 17, 20, 23} {
		if hour < h {
			return timePtr(t.Add(time.Duration(h) * time.Hour))
		}
	}
	return timePtr(t.Add(26 * time.Hour))
}

func timePtr(t time.Time) *time.Time { return &t }

func getDbInfo() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := postgres.Pool.Query(ctx, `
		SELECT category, COUNT(*) FROM media_cards
		WHERE category IS NOT NULL
		GROUP BY category ORDER BY category`)
	if err != nil {
		log.Println("getDbInfo:", err)
		return
	}
	defer rows.Close()

	total := 0
	for rows.Next() {
		var cat string
		var n int
		rows.Scan(&cat, &n) //nolint:errcheck
		fmt.Printf("%-20s %d\n", cat+":", n)
		total += n
	}

	var cached int
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM torrents`).Scan(&cached) //nolint:errcheck
	fmt.Println("Total media cards:", total)
	fmt.Println("Torrent cache entries:", cached)
}
