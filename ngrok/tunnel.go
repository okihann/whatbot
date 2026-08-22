package ngrok

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	ngrokgo "golang.ngrok.com/ngrok/v2"
)

// Tunnel owns the ngrok agent and endpoint so the tunnel can be explicitly
// started and stopped by the application lifecycle.
type Tunnel struct {
	mu       sync.Mutex
	agent    ngrokgo.Agent
	forward  ngrokgo.EndpointForwarder
	ctx      context.Context
	cancel   context.CancelFunc
}

// Config is deliberately small and environment-driven so tunnel behavior is
// visible and auditable in .env rather than hidden in the binary.
type Config struct {
	Enabled   bool
	Domain    string
	AuthToken string
	Port      string
}

func LoadConfig(port string) Config {
	return Config{
		Enabled:   parseBool(os.Getenv("NGROK_ENABLED")),
		Domain:    strings.TrimSpace(os.Getenv("NGROK_DOMAIN")),
		AuthToken: strings.TrimSpace(os.Getenv("NGROK_AUTHTOKEN")),
		Port:      normalizePort(port),
	}
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

func normalizePort(port string) string {
	port = strings.TrimSpace(port)
	return strings.TrimPrefix(port, ":")
}

// Start creates an ngrok endpoint only when explicitly enabled. It forwards
// to the bot's local HTTP server and never downloads or launches an ngrok
// executable.
func (t *Tunnel) Start(cfg Config) error {
	if !cfg.Enabled {
		log.Println("🔒 [NGROK] Disabled (NGROK_ENABLED=false).")
		return nil
	}
	if cfg.AuthToken == "" {
		return fmt.Errorf("NGROK_ENABLED=true but NGROK_AUTHTOKEN is empty")
	}
	if cfg.Domain == "" {
		return fmt.Errorf("NGROK_ENABLED=true but NGROK_DOMAIN is empty")
	}
	if cfg.Port == "" {
		return fmt.Errorf("NGROK_ENABLED=true but server port is empty")
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.forward != nil {
		return fmt.Errorf("ngrok tunnel is already running")
	}

	t.ctx, t.cancel = context.WithCancel(context.Background())
	agent, err := ngrokgo.NewAgent(
		ngrokgo.WithAuthtoken(cfg.AuthToken),
		ngrokgo.WithAgentDescription("whatbot explicit ngrok tunnel"),
	)
	if err != nil {
		t.cancel()
		t.cancel = nil
		t.ctx = nil
		return fmt.Errorf("create ngrok agent: %w", err)
	}

	forward, err := agent.Forward(
		t.ctx,
		ngrokgo.WithUpstream("http://127.0.0.1:"+cfg.Port),
		ngrokgo.WithURL(cfg.Domain),
		ngrokgo.WithDescription("whatbot HTTP API tunnel"),
		ngrokgo.WithName("whatbot"),
	)
	if err != nil {
		t.cancel()
		t.cancel = nil
		t.ctx = nil
		return fmt.Errorf("start ngrok endpoint: %w", err)
	}

	t.agent = agent
	t.forward = forward
	log.Printf("🌐 [NGROK] Enabled: https://%s -> http://127.0.0.1:%s\n", cfg.Domain, cfg.Port)
	return nil
}

// Stop explicitly closes the endpoint and disconnects the agent.
func (t *Tunnel) Stop() error {
	t.mu.Lock()
	forward := t.forward
	agent := t.agent
	cancel := t.cancel
	t.forward = nil
	t.agent = nil
	t.cancel = nil
	t.ctx = nil
	t.mu.Unlock()

	if forward == nil && agent == nil {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	if forward != nil {
		if err := forward.Close(); err != nil {
			return fmt.Errorf("close ngrok endpoint: %w", err)
		}
	}
	if agent != nil {
		if err := agent.Disconnect(); err != nil {
			return fmt.Errorf("disconnect ngrok agent: %w", err)
		}
	}
	log.Println("🔒 [NGROK] Tunnel stopped.")
	return nil
}

func (t *Tunnel) Enabled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.forward != nil
}
