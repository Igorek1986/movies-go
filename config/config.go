package config

import "github.com/ilyakaznacheev/cleanenv"

type ConfigParser struct {
	// БД
	DatabaseURL string `env:"DATABASE_URL"`

	// Парсер
	Host      string `env:"HOST_RUTOR" env-default:"http://rutor.info"`
	TmdbToken string `env:"TMDB_TOKEN"`

	// HTTP сервер
	HTTPPort int `env:"PORT" env-default:"8080"`

	// Суперпользователь (создаётся при старте)
	SuperUsername string `env:"SUPERUSER_USERNAME"`
	SuperPassword string `env:"SUPERUSER_PASSWORD"`
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
