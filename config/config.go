package config

import "github.com/ilyakaznacheev/cleanenv"

type ConfigParser struct {
	// БД
	DatabaseURL string `env:"DATABASE_URL"`

	// Парсер
	TmdbToken string `env:"TMDB_TOKEN"`

	// HTTP сервер
	HTTPPort int `env:"PORT" env-default:"8888"`

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
