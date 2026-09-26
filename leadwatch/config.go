package leadwatch

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGroqModel = "openai/gpt-oss-20b"
	maxSearchPosts   = 8
	maxNewPerCycle   = 20
	maxBatchSize     = 10
	minLeadScore     = 70
)

var defaultQueries = []string{
	"нужен разработчик сайт",
	"ищу разработчика сайта",
	"ищу интегратора amoCRM",
	"нужен интегратор amoCRM",
	"looking for web developer",
	"need a web developer",
	"looking for CRM automation",
	"need someone to build an app",
}

type Config struct {
	DatabaseURL  string
	GroqAPIKey   string
	GroqModel    string
	TelegramBot  string
	TelegramChat string
	OfferProfile string
	Queries      []string
	Interval     time.Duration
	Port         string
}

func ConfigFromEnv() (Config, error) {
	cfg := Config{
		DatabaseURL:  strings.TrimSpace(os.Getenv("DATABASE_URL")),
		GroqAPIKey:   strings.TrimSpace(os.Getenv("GROQ_API_KEY")),
		GroqModel:    strings.TrimSpace(os.Getenv("GROQ_MODEL")),
		TelegramBot:  strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		TelegramChat: strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")),
		OfferProfile: strings.TrimSpace(os.Getenv("LEADS_OFFER_PROFILE")),
		Port:         strings.TrimSpace(os.Getenv("PORT")),
	}
	if cfg.GroqModel == "" {
		cfg.GroqModel = defaultGroqModel
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	intervalMinutes := 60
	if raw := strings.TrimSpace(os.Getenv("LEADS_INTERVAL_MINUTES")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 15 || parsed > 1440 {
			return Config{}, errors.New("LEADS_INTERVAL_MINUTES must be between 15 and 1440")
		}
		intervalMinutes = parsed
	}
	cfg.Interval = time.Duration(intervalMinutes) * time.Minute
	cfg.Queries = parseQueries(os.Getenv("LEADS_QUERIES"))
	if len(cfg.Queries) == 0 {
		cfg.Queries = append([]string(nil), defaultQueries...)
	}
	for name, value := range map[string]string{
		"DATABASE_URL": cfg.DatabaseURL, "GROQ_API_KEY": cfg.GroqAPIKey,
		"TELEGRAM_BOT_TOKEN": cfg.TelegramBot, "TELEGRAM_CHAT_ID": cfg.TelegramChat,
	} {
		if value == "" {
			return Config{}, fmt.Errorf("%s is required", name)
		}
	}
	return cfg, nil
}

func parseQueries(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '|' })
	queries := make([]string, 0, len(parts))
	seen := make(map[string]bool)
	for _, part := range parts {
		query := strings.TrimSpace(part)
		key := strings.ToLower(query)
		if query == "" || seen[key] {
			continue
		}
		seen[key] = true
		queries = append(queries, query)
	}
	return queries
}
