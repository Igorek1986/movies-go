package config

import "github.com/ilyakaznacheev/cleanenv"

var ProxyHost = ""
var UseProxy = false

type ConfigParser struct {
	// БД
	DatabaseURL string `yaml:"database_url" env:"DATABASE_URL"`

	// Парсер
	Host      string `yaml:"host"      env:"HOST_RUTOR"     env-default:"http://rutor.info"`
	UseProxy  string `yaml:"useproxy"  env:"USEPROXY_RUTOR" env-default:"false"`
	Proxy     string `yaml:"proxy"     env:"PROXY_RUTOR"    env-default:""`
	TmdbToken string `yaml:"tmdbtoken" env:"TMDB_TOKEN"`

	// HTTP сервер
	HTTPPort int    `yaml:"http_port" env:"HTTP_PORT" env-default:"8080"`
	BaseURL  string `yaml:"base_url"  env:"BASE_URL"  env-default:""`

	// Суперпользователь (создаётся при старте)
	SuperUsername string `yaml:"super_username" env:"SUPERUSER_USERNAME"`
	SuperPassword string `yaml:"super_password" env:"SUPERUSER_PASSWORD"`

	// Telegram
	TelegramBotToken   string `yaml:"telegram_bot_token"    env:"TELEGRAM_BOT_TOKEN"`
	TelegramBotName    string `yaml:"telegram_bot_name"     env:"TELEGRAM_BOT_NAME"`
	TelegramAdminIDs   string `yaml:"telegram_admin_ids"    env:"TELEGRAM_ADMIN_IDS"`
	TelegramUsePolling bool   `yaml:"telegram_use_polling"  env:"TELEGRAM_USE_POLLING"`

	// Веб-панель
	AdminUsernames string `yaml:"admin_usernames" env:"ADMIN_USERNAMES"`
	AdminPassword  string `yaml:"admin_password"  env:"ADMIN_PASSWORD"`

	// MyShows
	MyShowsAPI     string `yaml:"myshows_api"      env:"MYSHOWS_API"      env-default:"https://api.myshows.me/v3"`
	MyShowsAuthURL string `yaml:"myshows_auth_url" env:"MYSHOWS_AUTH_URL" env-default:"https://auth.myshows.me"`

	// Контент
	BannedPatterns     string `yaml:"banned_patterns"      env:"BANNED_PATTERNS"`
	CacheClearPassword string `yaml:"cache_clear_password" env:"CACHE_CLEAR_PASSWORD"`
	PluginURL          string `yaml:"plugin_url"           env:"PLUGIN_URL"`
	DonateURL          string `yaml:"donate_url"           env:"DONATE_URL"`
	SiteName           string `yaml:"site_name"            env:"SITE_NAME"  env-default:"lampa-api"`
	ContactEmail       string `yaml:"contact_email"        env:"CONTACT_EMAIL"`

	// Прокси для изображений
	ImageProxyURL  string `yaml:"image_proxy_url"  env:"IMAGE_PROXY_URL"`
	ImageProxyUser string `yaml:"image_proxy_user" env:"IMAGE_PROXY_USER"`
	ImageProxyPass string `yaml:"image_proxy_pass" env:"IMAGE_PROXY_PASS"`
}

var cfg ConfigParser
var loaded bool

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
