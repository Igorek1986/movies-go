package config

import "github.com/ilyakaznacheev/cleanenv"

var ProxyHost = ""

type ConfigParser struct {
	// БД
	DatabaseURL string `env:"DATABASE_URL"`

	// Парсер
	Host      string `env:"HOST_RUTOR"      env-default:"http://rutor.info"`
	Proxy          string `env:"PROXY_RUTOR"`
	ProxyRutorUser string `env:"PROXY_RUTOR_USER"`
	ProxyRutorPass string `env:"PROXY_RUTOR_PASS"`
	TmdbToken string `env:"TMDB_TOKEN"`
	ProxyURL  string `env:"PROXY_URL"`
	ProxyUser string `env:"PROXY_USER"`
	ProxyPass string `env:"PROXY_PASS"`

	// HTTP сервер
	HTTPPort int    `env:"HTTP_PORT" env-default:"8080"`
	BaseURL  string `env:"BASE_URL"`

	// Суперпользователь (создаётся при старте)
	SuperUsername string `env:"SUPERUSER_USERNAME"`
	SuperPassword string `env:"SUPERUSER_PASSWORD"`

	// Telegram
	TelegramBotToken   string `env:"TELEGRAM_BOT_TOKEN"`
	TelegramBotName    string `env:"TELEGRAM_BOT_NAME"`
	TelegramAdminIDs   string `env:"TELEGRAM_ADMIN_IDS"`
	TelegramUsePolling bool   `env:"TELEGRAM_USE_POLLING"`

	// Веб-панель
	AdminUsernames string `env:"ADMIN_USERNAMES"`
	AdminPassword  string `env:"ADMIN_PASSWORD"`

	// MyShows
	MyShowsAPI     string `env:"MYSHOWS_API"      env-default:"https://myshows.me/v3/rpc/"`
	MyShowsAuthURL string `env:"MYSHOWS_AUTH_URL" env-default:"https://myshows.me/api/session"`

	// Режим запуска: "all" (по умолчанию) или "parser" (только контент API, без web/auth)
	AppMode string `env:"APP_MODE" env-default:"all"`

	// Контент
	BannedPatterns     string `env:"BANNED_PATTERNS"`
	CacheClearPassword string `env:"CACHE_CLEAR_PASSWORD"`
	PluginURL          string `env:"PLUGIN_URL"`
	DonateURL          string `env:"DONATE_URL"`
	SiteName           string `env:"SITE_NAME" env-default:"movies-api"`
	ContactEmail       string `env:"CONTACT_EMAIL"`

}

var cfg ConfigParser
var loaded bool

func Get() *ConfigParser {
	if !loaded {
		cleanenv.ReadEnv(&cfg) //nolint:errcheck
		loaded = true
	}
	return &cfg
}
