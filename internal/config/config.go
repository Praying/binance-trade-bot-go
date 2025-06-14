package config

import (
	"strings"

	"github.com/spf13/viper"
)

// Config holds all configuration for the application.
type Config struct {
	Binance  Binance  `mapstructure:"binance"`
	Trading  Trading  `mapstructure:"trading"`
	Logger   Logger   `mapstructure:"logger"`
	Server   Server   `mapstructure:"server"`
	Database Database `mapstructure:"database"`
}

// Binance holds the configuration for the Binance API.
type Binance struct {
	ApiKey         string  `mapstructure:"apiKey"`
	SecretKey      string  `mapstructure:"secretKey"`
	Testnet        bool    `mapstructure:"testnet"`
	RateLimit      float64 `mapstructure:"rate_limit"`
	RateLimitBurst int     `mapstructure:"rate_limit_burst"`
}

// Server holds the configuration for the web server.
type Server struct {
	Port int `mapstructure:"port"`
}

// Database holds the configuration for the database.
type Database struct {
	DSN string `mapstructure:"dsn"`
}

// Trading holds the configuration for the trading logic.
type Trading struct {
	Bridge       string   `mapstructure:"bridge"`
	TradePairs   []string `mapstructure:"trade_pairs"`
	Quantity     float64  `mapstructure:"quantity"`
	FeeRate      float64  `mapstructure:"fee_rate"`
	DryRun       bool     `mapstructure:"dry_run"`
	TickInterval int      `mapstructure:"tick_interval"`
	ScoutMargin  float64  `mapstructure:"scout_margin"`
	Strategy     string   `mapstructure:"strategy"`
}

// Logger holds the configuration for the logger.
type Logger struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// LoadConfig reads configuration from file or environment variables.
func LoadConfig(path string) (config Config, err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("config") // name of config file (without extension)
	viper.SetConfigType("yml")    // or yaml, json

	// Allow environment variables to override config file
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Set default values
	viper.SetDefault("binance.rate_limit", 20)      // requests per second
	viper.SetDefault("binance.rate_limit_burst", 5) // burst size

	err = viper.ReadInConfig()
	if err != nil {
		return
	}

	err = viper.Unmarshal(&config)
	return
}
