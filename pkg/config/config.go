package config

import (
	"fmt"

	"github.com/md-shajib/gogo-backend/pkg/log"
	"github.com/spf13/viper"
)

type Option struct {
	Verbose        bool
	PrettyPrintlog bool
}

func Init(cfgFile string, opts ...Option) error {
	// Setup viper with config path and environment prefix
	setupViper(cfgFile)

	// read config file
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("fail to read config file: %w", err)
	}

	// load .env file only in local/development environment
	loadEnvFileIfNeeded()

	// NEW: Apply cli flag overrides in provided
	if len(opts) > 0 {
		applyFlagOverrides(opts[0])
	}

	// setup logging based on config
	log.InitLogger()
	log.InitSentry()


	return nil
}
