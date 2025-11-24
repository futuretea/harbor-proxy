package main

import (
	"github.com/rs/zerolog/log"

	"github.com/futuereta/harbor-proxy/pkg/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("harbor-proxy failed")
	}
}
