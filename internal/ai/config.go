// Package ai implements a conversational flow that turns a geometry/Math
// problem (given as text or an image) into GeoGebra dynamic-geometry
// instructions, using an OpenAI-compatible chat backend. Generated scripts are
// gated through the existing ggbcheck validator and auto-repaired in a bounded
// loop, so the final output is executable in GeoGebra for teaching demos.
package ai

import (
	"os"
	"strconv"
	"strings"
)

// Config holds the knobs for the AI conversation service. Every field is
// overridable through GGCM_AI_* environment variables (see LoadConfig).
type Config struct {
	Endpoint       string // OpenAI-compatible base URL, e.g. https://token.sensenova.cn/v1/
	Model          string // e.g. sensenova-6.8-flash-lite, deepseek-chat
	APIKey         string // may be empty for some local compatible backends
	Temperature    float64
	MaxTokens      int
	MaxRepair      int     // retry rounds after the first failed generation
	MaxImageBytes  int     // cap on inline image payload (bytes)
	DisableVision  bool    // refuse image input even if the backend might support it
	MaxHistory     int     // session turns kept in context
	HTTPTimeoutS   int     // per LLM call timeout in seconds
	HTTPRetries    int     // max exponential-backoff retries on transient (non-200) statuses
	HTTPRetryBase  int     // initial backoff in ms; doubles each retry
	RequestBudgetS int     // per /api/chat request budget (whole repair loop) in seconds
	Log            LogFunc // optional structured-log hook; nil disables logging
}

// LoadConfig reads configuration from GGCM_AI_* environment variables, filling
// in the documented defaults where absent.
func LoadConfig() Config {
	return Config{
		Endpoint:       envOr("GGCM_AI_ENDPOINT", "https://token.sensenova.cn/v1/"),
		Model:          envOr("GGCM_AI_MODEL", "sensenova-6.8-flash-lite"),
		APIKey:         os.Getenv("GGCM_AI_API_KEY"),
		Temperature:    envFloat("GGCM_AI_TEMP", 0.2),
		MaxTokens:      envInt("GGCM_AI_MAX_TOKENS", 16384),
		MaxRepair:      envInt("GGCM_AI_MAX_REPAIR", 3),
		MaxImageBytes:  envInt("GGCM_AI_MAX_IMAGE_BYTES", 10<<20), // 10 MiB
		DisableVision:  envBool("GGCM_AI_DISABLE_VISION"),
		MaxHistory:     envInt("GGCM_AI_MAX_HISTORY", 20),
		HTTPTimeoutS:   envInt("GGCM_AI_HTTP_TIMEOUT_S", 300),
		HTTPRetries:    envInt("GGCM_AI_HTTP_RETRIES", 5),
		HTTPRetryBase:  envInt("GGCM_AI_HTTP_RETRY_BASE_MS", 500),
		RequestBudgetS: envInt("GGCM_AI_REQUEST_TIMEOUT_S", 900),
	}
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envBool(key string) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return false
	}
	b, _ := strconv.ParseBool(v)
	return b
}
