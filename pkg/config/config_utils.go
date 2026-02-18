package config

import (
	"os"

	"github.com/joho/godotenv"
	"github.com/md-shajib/gogo-backend/pkg/log"
	"github.com/spf13/viper"
)

// Environment Constant
const (
	EnvLocal     = "local"
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

func setupViper(cfgFile string) {
	viper.SetEnvPrefix("gogo")
	viper.AutomaticEnv()

	if cfgFile != "" {
		// use explicitely provided config file
		viper.SetConfigFile(cfgFile)
	} else {
		// use default config file
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("./config")
	}
}

func loadEnvFileIfNeeded() {
	env := viper.GetString("app.environment")

	// skip loading .env file in production
	if env == EnvProduction {
		return
	}

	// Attempt to load .env file, ignore error if file not found
	if _, err := os.Stat(".env"); err == nil {
		_ = godotenv.Load()
	}
}

func applyFlagOverrides(opts Option) {
	if opts.Verbose {
		viper.Set("log.level", "trace")
		log.Debug(nil, "Verbose mode enabled via CLI flag")
	}

	if opts.PrettyPrintlog {
		viper.Set("log.pretty_print", true)
		log.Debug(nil, "Pretty print log enabled via CLI flag")
	}
}