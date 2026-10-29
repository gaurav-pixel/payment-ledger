package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DBHost      string
	DBPort      int
	DBUser      string
	DBPassword  string
	DBName      string
	DBSSLMode   string
	DBMaxConns  int32
	DBMinConns  int32
}

func LoadConfig() *Config {
	_ = godotenv.Load()

	port := getEnv("PORT", "8080")
	dbHost := getEnv("DB_HOST", "localhost")
	dbPortStr := getEnv("DB_PORT", "5432")
	dbPort, _ := strconv.Atoi(dbPortStr)
	if dbPort == 0 {
		dbPort = 5432
	}

	dbUser := getEnv("DB_USER", "postgres")
	dbPass := getEnv("DB_PASSWORD", "postgres")
	dbName := getEnv("DB_NAME", "ledger_db")
	dbSSL := getEnv("DB_SSLMODE", "disable")

	maxConnsStr := getEnv("DB_MAX_CONNS", "25")
	maxConns, _ := strconv.Atoi(maxConnsStr)
	if maxConns == 0 {
		maxConns = 25
	}

	minConnsStr := getEnv("DB_MIN_CONNS", "5")
	minConns, _ := strconv.Atoi(minConnsStr)
	if minConns == 0 {
		minConns = 5
	}

	return &Config{
		Port:       port,
		DBHost:     dbHost,
		DBPort:     dbPort,
		DBUser:     dbUser,
		DBPassword: dbPass,
		DBName:     dbName,
		DBSSLMode:  dbSSL,
		DBMaxConns: int32(maxConns),
		DBMinConns: int32(minConns),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
