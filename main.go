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
	defaultPort       = 8080
	defaultEnvFile    = ""
	defaultTimeout    = 30 * time.Second
	defaultInline     = ""
	defaultScriptFile = ""

	envPortKey       = "PORT"
	envInlineKey     = "SCRIPT"
	envScriptFileKey = "SCRIPT_FILE"
	envEnvFileKey    = "ENV_FILE"
	envTimeoutKey    = "TIMEOUT_DURATION"
)

type Config struct {
	Port         int
	InlineScript string
	ScriptFile   string
	EnvFile      string
	Timeout      time.Duration
}

func (c *Config) LoadEnv() {
	if v := os.Getenv(envPortKey); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			log.Fatalf("invalid %s %q: %v", envPortKey, v, err)
		}
		c.Port = p
	}

	if v := os.Getenv(envInlineKey); v != "" {
		c.InlineScript = v
	}

	if v := os.Getenv(envScriptFileKey); v != "" {
		c.ScriptFile = v
	}

	if c.InlineScript != "" && c.ScriptFile != "" {
		log.Fatalf("must provide only one of %s or %s, not both", envInlineKey, envScriptFileKey)
	}

	if v := os.Getenv(envEnvFileKey); v != "" {
		c.EnvFile = v
	}

	if v := os.Getenv(envTimeoutKey); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			log.Fatalf("invalid %s %q: %v", envTimeoutKey, v, err)
		}
		c.Timeout = d
	}
}

func (c *Config) LoadFlags() {
	flag.IntVar(&c.Port, "port", c.Port, "port to listen on")

	flag.StringVar(&c.InlineScript, "script", c.InlineScript,
		"inline JavaScript to evaluate (mutually exclusive with --script-file)")
	flag.StringVar(&c.ScriptFile, "script-file", c.ScriptFile,
		"path to JavaScript file to run (mutually exclusive with --script)")

	flag.StringVar(&c.EnvFile, "env-file", c.EnvFile,
		"path to .env file for the script (optional)")
	flag.DurationVar(&c.Timeout, "timeout", c.Timeout,
		"timeout for node invocation (e.g. 30s, 1m)")

	flag.Parse()

	if c.InlineScript != "" && c.ScriptFile != "" {
		log.Fatal("must provide only one of --script or --script-file, not both")
	}
}

func main() {
	cfg := Config{
		Port:         defaultPort,
		InlineScript: defaultInline,
		ScriptFile:   defaultScriptFile,
		EnvFile:      defaultEnvFile,
		Timeout:      defaultTimeout,
	}

	cfg.LoadEnv()
	cfg.LoadFlags()
	if (cfg.InlineScript == "") == (cfg.ScriptFile == "") {
		log.Fatalf("must provide exactly one of --script or --script-file (or via %s, %s environment variables)", envInlineKey, envScriptFileKey)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Starting server on %s (timeout=%s)…", addr, cfg.Timeout)

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

		args := []string{}

		if cfg.EnvFile != "" {
			args = append(args, "--env-file", cfg.EnvFile)
		}

		if cfg.InlineScript != "" {
			args = append(args, "-e", cfg.InlineScript)
		} else {
			args = append(args, cfg.ScriptFile)
		}

		cmd := exec.CommandContext(ctx, "node", args...)
		cmd.Stdin = bytes.NewReader(payload)

		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf

		if err := cmd.Run(); err != nil {
			log.Println(outBuf.String())
			log.Printf("node error: %v, stderr: %s", err, errBuf.String())
			http.Error(w,
				"node.js failed: "+firstLine(errBuf.String(), err.Error()),
				http.StatusInternalServerError,
			)
			return
		}
		log.Println(outBuf.String())

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
