package friday

import (
	"context"
	"fmt"
	"time"
)

type Service struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Tagline string `json:"tagline"`
	Status  string `json:"status"`
	Details string `json:"details,omitempty"`
	Icon    string `json:"icon"`
	Slug    string `json:"slug"`
}

func (s *Server) GetServices(ctx context.Context) []Service {
	return []Service{
		s.svcFriday(ctx),
		s.svcHealer(ctx),
		s.svcBridge(ctx),
	}
}

func (s *Server) svcFriday(ctx context.Context) Service {
	return Service{
		Name: "Friday", Role: "Core",
		Tagline: "autonomous AI — tools, trading, chat",
		Status: "online", Icon: "brain", Slug: "friday",
		Details: fmt.Sprintf("%d tools, %d workers", len(s.registry.Schemas()), activeWorkerCount()),
	}
}

func (s *Server) svcHealer(ctx context.Context) Service {
	status := "online"
	details := "Self-healing active"
	if s.healer == nil { status = "offline"; details = "Not initialized" }
	return Service{
		Name: "Healer", Role: "Monitor",
		Tagline: "keeps everything running",
		Status: status, Icon: "heart", Slug: "healer",
		Details: details,
	}
}

func (s *Server) svcBridge(ctx context.Context) Service {
	llmOk := false
	hc, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.llmClient.Health(hc); err == nil { llmOk = true }
	status := "online"
	details := "DeepSeek-V4-Pro via DeepInfra"
	if !llmOk { status = "degraded"; details = "Bridge unreachable, fallback active" }
	return Service{
		Name: "Bridge", Role: "AI",
		Tagline: "connects Friday to DeepSeek",
		Status: status, Icon: "link", Slug: "bridge",
		Details: details,
	}
}
