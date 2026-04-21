package main

import (
	"context"
	"fmt"
	"lampa-api/config"
	"lampa-api/db"
	"lampa-api/db/postgres"
	"lampa-api/movies/tmdb"
	"lampa-api/parser"
	"lampa-api/version"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

	fmt.Println("=========== START ===========")
	fmt.Println("lm_"+version.Version+",", runtime.Version()+",", "CPU Num:", runtime.NumCPU())

	if params.Proxy != "" {
		_, err := url.Parse(params.Proxy)
		if err != nil {
			log.Println("Error parse proxy host:", err)
		} else {
			config.ProxyHost = params.Proxy
		}
	} else {
		proxy, err := config.ReadConfigParser("Proxy")
		if err == nil {
			params.Proxy = proxy
			_, err := url.Parse(params.Proxy)
			if err == nil {
				config.ProxyHost = params.Proxy
			} else {
				log.Println("Error parse proxy host:", err)
			}
		}
	}

	if params.UseProxy {
		config.UseProxy = params.UseProxy
	} else {
		use_proxy, err := config.ReadConfigParser("UseProxy")
		if err == nil && use_proxy == "true" {
			params.UseProxy = true
		} else {
			params.UseProxy = false
		}
	}

	dnsResolve()

	db.Init()
	loadProxy()
	tmdb.Init()

	getDbInfo()

	scanReleases()

	log.Println("Start timer")
	gocron.Every(3).Hours().From(calcTime()).Do(scanReleases)
	<-gocron.Start()
}

func dnsResolve() {
	hosts := [6]string{"1.1.1.1", "1.0.0.1", "208.67.222.222", "208.67.220.220", "8.8.8.8", "8.8.4.4"}
	ret := 0
	for _, ip := range hosts {
		ret = toolResolve("www.google.com", ip)
		switch {
		case ret == 2:
			fmt.Println("DNS resolver OK")
		case ret == 1:
			fmt.Println("New DNS resolver OK")
		case ret == 0:
			fmt.Println("New DNS resolver failed")
		}
		if ret == 2 || ret == 1 {
			break
		}
	}
}

func toolResolve(host string, serverDNS string) int {
	addrs, err := net.LookupHost(host)
	addr_dns := fmt.Sprintf("%s:53", serverDNS)
	a := 0
	if len(addrs) == 0 {
		fmt.Println("Check dns", addrs, err)
		fn := func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "udp", addr_dns)
		}
		net.DefaultResolver = &net.Resolver{
			Dial: fn,
		}
		addrs, err = net.LookupHost(host)
		fmt.Println("Check new dns", addrs, err)
		if err == nil || len(addrs) > 0 {
			a = 1
		} else {
			a = 0
		}
	} else {
		a = 2
	}
	return a
}

func scanReleases() {
	loadProxy()
	rutorParser := parser.NewRutor()
	rutorParser.Parse()
	getDbInfo()
}

// loadProxy runs proxy.sh to populate proxy.list when UseProxy is enabled.
func loadProxy() {
	if config.UseProxy {
		log.Println("Load proxy list...")
		dir := filepath.Dir(os.Args[0])
		logOut, err := exec.Command("/bin/sh", filepath.Join(dir, "proxy.sh")).CombinedOutput()
		if err != nil {
			log.Println("Error loading proxy:", err)
		}
		log.Println(string(logOut))
	}
}

func calcTime() *time.Time {
	//2 5 8 11 14 17 20 23
	hour := time.Now().Hour()
	t := time.Date(time.Now().Year(),
		time.Now().Month(),
		time.Now().Day(),
		0, 0, 0, 0, time.Local)
	if hour < 2 {
		t = t.Add(2 * time.Hour)
	} else if hour < 5 {
		t = t.Add(5 * time.Hour)
	} else if hour < 11 {
		t = t.Add(11 * time.Hour)
	} else if hour < 14 {
		t = t.Add(14 * time.Hour)
	} else if hour < 17 {
		t = t.Add(17 * time.Hour)
	} else if hour < 20 {
		t = t.Add(20 * time.Hour)
	} else if hour < 23 {
		t = t.Add(23 * time.Hour)
	} else if hour >= 23 {
		t = t.Add(26 * time.Hour)
	}
	return &t
}

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
		rows.Scan(&cat, &n)
		fmt.Printf("%-20s %d\n", cat+":", n)
		total += n
	}

	var cached int
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM torrent_cache`).Scan(&cached)
	fmt.Println("Total media cards:", total)
	fmt.Println("Torrent cache entries:", cached)
}
