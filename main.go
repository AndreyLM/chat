package main

import (
	"log"

	"github.com/andreylm/chat/server"
	"github.com/kelseyhightower/envconfig"
)

type config struct {
	RedisURL string `envconfig:"REDIS_URL"`
}

func main() {
	var cfg config
	err := envconfig.Process("", &cfg)
	if err != nil {
		log.Fatal(err)
	}

	s, err := server.NewGraphQlServer(cfg.RedisURL)
	if err != nil {
		log.Fatal(err)
	}
	err = s.Serve("/graphql", 808)
	if err != nil {
		log.Fatal(err)
	}
}
