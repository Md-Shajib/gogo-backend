package log

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// initialization
var logger = logrus.New()

// Debug logs a message at level Debug on the standard logger.
func Debug(args ...interface{}){
	if logger.Level >= logrus.DebugLevel {
		entry := logger.WithFields(logrus.Fields{})
		entry.Data["file"] = fileInfo(2)
		entry.Debug(args...)
	}
}

// 
func fileInfo(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		file = "<???>"
		line = 1
	} else {
		slash := strings.LastIndex(file, "/")
		if slash >= 0 {
			file = file[slash+1:]
		}
	}
	return fmt.Sprintf("%s:%d", file, line)
}

// InitLogger initializes the logger based on configuration settings.
// Call this after config is loaded
func InitLogger() {
	logLevel := viper.GetString("log.level")
	if logLevel == "" {
		logLevel = "info" // default log level
	}

	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		logger.Warnf("Invalid log level '%s', defaulting to 'info'", logLevel)
		level = logrus.InfoLevel
	}
	SetLogLevel(level)
}

// Set Log Level at runtime
func SetLogLevel(level logrus.Level) {
	logger.SetLevel(level)
}

// InitSentry initializes Sentry error tracking based on configuration settings.
func InitSentry() {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		logger.Info("Sentry DSN not set, skipping Sentry initialization")
		return
	}
	logger.Info("Initializing Sentry with DSN: ", dsn)

	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		AttachStacktrace: true,
	}); err != nil {
		logger.Errorf("Sentry initialization failed: %v", err)
	} else {
		logger.Info("Sentry initialized successfully")
	}
}