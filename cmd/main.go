package main

import (
	"context"
	"fmt"
	"lampa-api/config"
	"lampa-api/db"
	"lampa-api/db/postgres"
	"lampa-api/db/store"
	"lampa-api/internal/api"
	"lampa-api/internal/auth"
	"lampa-api/movies/tmdb"
	"lampa-api/parser"
	"lampa-api/version"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/jasonlvhit/gocron"
)

type args struct {
	Proxy    string `arg:"--proxy" help:"proxy for rutor, http://user:password@ip:port"`
	UseProxy bool   `arg:"--useproxy" help:"enable auto proxy"`
}

var params args

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	arg.MustParse(&params)

	cfg := config.Get()

	fmt.Println("=========== START ===========")
	fmt.Printf("lm_%s, %s, CPU: %d\n", version.Version, runtime.Version(), runtime.NumCPU())

	setupProxy(cfg)
	dnsResolve()

	db.Init()
	ensureSuperuser(cfg)
	loadProxy()
	tmdb.Init()

	getDbInfo()

	// HTTP сервер
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:      api.NewRouter(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("HTTP server listening on :%d", cfg.HTTPPort)
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv.Shutdown(ctx) //nolint:errcheck
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

func setupProxy(cfg *config.ConfigParser) {
	if params.Proxy != "" {
		if _, err := url.Parse(params.Proxy); err == nil {
			config.ProxyHost = params.Proxy
		}
	} else if cfg.Proxy != "" {
		if _, err := url.Parse(cfg.Proxy); err == nil {
			config.ProxyHost = cfg.Proxy
		}
	}

	if params.UseProxy || cfg.UseProxy == "true" {
		config.UseProxy = true
	}
}

func dnsResolve() {
	hosts := []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4"}
	for _, ip := range hosts {
		if tryDNS("www.google.com", ip) {
			return
		}
	}
}

func tryDNS(host, serverDNS string) bool {
	addrs, _ := net.LookupHost(host)
	if len(addrs) > 0 {
		return true
	}
	net.DefaultResolver = &net.Resolver{
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", serverDNS+":53")
		},
	}
	addrs, _ = net.LookupHost(host)
	return len(addrs) > 0
}

func scanReleases() {
	loadProxy()
	parser.NewRutor().Parse()
	getDbInfo()
}

func loadProxy() {
	if config.UseProxy {
		log.Println("Load proxy list...")
		dir := filepath.Dir(os.Args[0])
		out, err := exec.Command("/bin/sh", filepath.Join(dir, "proxy.sh")).CombinedOutput()
		if err != nil {
			log.Println("Error loading proxy:", err)
		}
		log.Println(string(out))
	}
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
		SELECT rutor_category, COUNT(*) FROM media_cards
		WHERE rutor_category IS NOT NULL
		GROUP BY rutor_category ORDER BY rutor_category`)
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
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM torrent_cache`).Scan(&cached) //nolint:errcheck
	fmt.Println("Total media cards:", total)
	fmt.Println("Torrent cache entries:", cached)
}
