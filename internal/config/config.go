package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config armazena todas as variáveis de configuração necessárias para o Gateway rodar.
type Config struct {
	ServerPort        string
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	UpstreamURL       string
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
	HMACSecret        string
	HMACTolerance     time.Duration
	HMACProtectAll    bool
	AESKey            string
	StripPrefix       string
	RateLimitRequests int64
	RateLimitWindow   time.Duration
}

// Load lê as configurações a partir das variáveis de ambiente, valida campos obrigatórios
// e retorna uma instância de Config pronta para uso.
func Load() (*Config, error) {
	hmacTolerance, err := getEnvDuration("HMAC_TOLERANCE", 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to load HMAC_TOLERANCE: %w", err)
	}
	hmacProtectAll, err := getEnvBool("HMAC_PROTECT_ALL", false)
	if err != nil {
		return nil, fmt.Errorf("failed to load HMAC_PROTECT_ALL: %w", err)
	}
	rateLimitWindow, err := getEnvDuration("RATE_LIMIT_WINDOW", 1*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to load RATE_LIMIT_WINDOW: %w", err)
	}
	rateLimitRequests, err := getEnvInt64("RATE_LIMIT_REQUESTS", 100, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to load RATE_LIMIT_REQUESTS: %w", err)
	}
	redisDB, err := getEnvInt64("REDIS_DB", 0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to load REDIS_DB: %w", err)
	}

	hmacSecret := os.Getenv("HMAC_SECRET")
	if len(hmacSecret) < 32 {
		return nil, fmt.Errorf("environment variable HMAC_SECRET must be at least 32 characters long")
	}

	readTimeout, err := getEnvDuration("READ_TIMEOUT", 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to load READ_TIMEOUT: %w", err)
	}

	writeTimeout, err := getEnvDuration("WRITE_TIMEOUT", 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to load WRITE_TIMEOUT: %w", err)
	}

	idleTimeout, err := getEnvDuration("IDLE_TIMEOUT", 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to load IDLE_TIMEOUT: %w", err)
	}

	cfg := &Config{
		ServerPort:        getEnv("SERVER_PORT", "8080"),
		UpstreamURL:       getEnv("UPSTREAM_URL", "http://localhost:8081"),
		RedisAddr:         getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:     getEnv("REDIS_PASSWORD", ""),
		RedisDB:           int(redisDB),
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		HMACSecret:        hmacSecret,
		HMACTolerance:     hmacTolerance,
		HMACProtectAll:    hmacProtectAll,
		AESKey:            getEnv("AES_KEY", ""),
		StripPrefix:       getEnv("STRIP_PREFIX", ""),
		RateLimitRequests: rateLimitRequests,
		RateLimitWindow:   rateLimitWindow,
	}
	return cfg, nil
}

// getEnv busca uma variável de ambiente ou retorna um fallback.
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	if value := os.Getenv(key); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil {
			return 0, fmt.Errorf("failed to parse duration from environment variable %s: %w", key, err)
		}
		if d <= 0 {
			return 0, fmt.Errorf("environment variable %s must be a positive duration", key)
		}
		return d, nil
	}
	return fallback, nil
}

func getEnvInt64(key string, fallback, minValue int64) (int64, error) {
	if value := os.Getenv(key); value != "" {
		intValue, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse int64 from environment variable %s: %w", key, err)
		}
		if intValue < minValue {
			return 0, fmt.Errorf("environment variable %s must be at least %d", key, minValue)
		}
		return intValue, nil
	}
	return fallback, nil
}

func getEnvBool(key string, fallback bool) (bool, error) {
	if value := os.Getenv(key); value != "" {
		b, err := strconv.ParseBool(value)
		if err != nil {
			return false, fmt.Errorf("failed to parse bool from environment variable %s: %w", key, err)
		}
		return b, nil
	}
	return fallback, nil
}
