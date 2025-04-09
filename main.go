package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const (
	defaultPort     = 8080
	defaultFunction = "MyFunction"
	defaultEnvFile  = ""
	defaultTimeout  = 30 * time.Second
	defaultTemplate = "template.yaml"

	envPortKey     = "PORT"
	envFunctionKey = "LAMBDA_FUNCTION"
	envEnvFileKey  = "LAMBDA_ENV_FILE"
	envTimeoutKey  = "TIMEOUT_DURATION"
	envTemplateKey = "TEMPLATE_PATH"
)

type Config struct {
	Port     int
	Function string
	EnvFile  string
	Timeout  time.Duration
	Template string
}

func (c *Config) LoadEnv() {
	// PORT
	if v := os.Getenv(envPortKey); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			log.Fatalf("invalid %s %q: %v", envPortKey, v, err)
		}
		c.Port = p
	}

	// LAMBDA_FUNCTION
	if v := os.Getenv(envFunctionKey); v != "" {
		c.Function = v
	}

	// LAMBDA_ENV_FILE
	if v := os.Getenv(envEnvFileKey); v != "" {
		c.EnvFile = v
	}

	// TIMEOUT_DURATION
	if v := os.Getenv(envTimeoutKey); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			log.Fatalf("invalid %s %q: %v", envTimeoutKey, v, err)
		}
		c.Timeout = d
	}

	// TEMPLATE_PATH
	if v := os.Getenv(envTemplateKey); v != "" {
		c.Template = v
	}
}

func (c *Config) LoadFlags() {
	flag.IntVar(&c.Port, "port", c.Port, "port to listen on")
	flag.StringVar(&c.Function, "function", c.Function, "SAM logical function name to invoke")
	flag.StringVar(&c.EnvFile, "env-file", c.EnvFile, "path to JSON file of environment variables for sam local invoke")
	flag.DurationVar(&c.Timeout, "timeout", c.Timeout, "timeout for sam local invoke (e.g. 30s, 1m)")
	flag.StringVar(&c.Template, "template", c.Template, "path to SAM template.yaml for sam local invoke")
	flag.Parse()
}

func main() {
	// init with defaults
	cfg := Config{
		Port:     defaultPort,
		Function: defaultFunction,
		EnvFile:  defaultEnvFile,
		Timeout:  defaultTimeout,
		Template: defaultTemplate,
	}

	// override with env vars
	cfg.LoadEnv()

	// override with flags
	cfg.LoadFlags()

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf(
		"Starting server on %s (function=%s, env-file=%q, timeout=%s, template=%s)…",
		addr, cfg.Function, cfg.EnvFile, cfg.Timeout, cfg.Template,
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/invoke", makeInvokeHandler(cfg))

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}

func makeInvokeHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		payload, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if !json.Valid(payload) {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), cfg.Timeout)
		defer cancel()

		args := []string{
			"local", "invoke", cfg.Function,
			"--template", cfg.Template,
			"--event", "-",
		}
		if cfg.EnvFile != "" {
			args = append(args, "--env-vars", cfg.EnvFile)
		}

		cmd := exec.CommandContext(ctx, "sam", args...)
		cmd.Stdin = bytes.NewReader(payload)

		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf

		if err := cmd.Run(); err != nil {
			log.Printf("sam invoke error: %v, stderr: %s", err, errBuf.String())
			http.Error(w,
				"sam invoke failed: "+firstLine(errBuf.String(), err.Error()),
				http.StatusInternalServerError,
			)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(outBuf.Bytes())
	}
}

func firstLine(s, fallback string) string {
	for line := range bytes.SplitSeq([]byte(s), []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) > 0 {
			return string(line)
		}
	}
	return fallback
}
