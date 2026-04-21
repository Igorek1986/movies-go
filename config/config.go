package config

import "github.com/ilyakaznacheev/cleanenv"

// TODO: remove when releases/save.go and web/lampac.go are rewritten
const SaveReleasePath = "public/releases"
const ReleasesLimit = 0

var ProxyHost = ""
var UseProxy = false

type ConfigParser struct {
	// PostgreSQL connection string. Example:
	//   postgres://user:pass@localhost:5432/lmedia?sslmode=disable
	DatabaseURL string `yaml:"database_url" env:"DATABASE_URL"`

	Host      string `yaml:"host"      env:"HOST_RUTOR"     env-default:"http://rutor.info"`
	UseProxy  string `yaml:"useproxy"  env:"USEPROXY_RUTOR" env-defaults:"false"`
	Proxy     string `yaml:"proxy"     env:"PROXY_RUTOR"    env-default:""`
	TmdbToken string `yaml:"tmdbtoken" env:"TMDB_TOKEN"`
}

var cfg ConfigParser
var loaded bool

// Get returns the loaded config (reads config.yml once).
func Get() *ConfigParser {
	if !loaded {
		cleanenv.ReadConfig("config.yml", &cfg) //nolint:errcheck
		loaded = true
	}
	return &cfg
}

func ReadConfigParser(vars string) (string, error) {
	err := cleanenv.ReadConfig("config.yml", &cfg)
	if err == nil {
		switch vars {
		case "Host":
			return cfg.Host, nil
		case "Proxy":
			return cfg.Proxy, nil
		case "UseProxy":
			return cfg.UseProxy, nil
		case "TmdbToken":
			return cfg.TmdbToken, nil
		}
	}
	return "", err
}
