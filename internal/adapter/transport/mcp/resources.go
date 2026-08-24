package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// MCP resources (§2.6) — read-only, addressable views. Every read applies
// the same principal checks as the corresponding tools.

const (
	uriSpaceTemplate   = "memory://spaces/{space_id}"
	uriSessionTemplate = "memory://sessions/{session_id}"
	uriStats           = "memory://stats"
)

func spaceResource() mcp.ResourceTemplate {
	return mcp.NewResourceTemplate(uriSpaceTemplate, "Shared memory space metadata",
		mcp.WithTemplateDescription("SharedMemorySpace metadata: participants, access_policy, storage_backend, artifacts manifest, sync_state, last_synced_at"),
		mcp.WithTemplateMIMEType("application/json"))
}

func sessionResource() mcp.ResourceTemplate {
	return mcp.NewResourceTemplate(uriSessionTemplate, "Agent session state",
		mcp.WithTemplateDescription("AgentSession state: context_window budget, active_memories, injection_order, summary"),
		mcp.WithTemplateMIMEType("application/json"))
}

func statsResource() mcp.Resource {
	return mcp.NewResource(uriStats, "Global memory statistics",
		mcp.WithResourceDescription("Global GET /lifecycle/stats document (JSON), refreshed ≤ 60 s"),
		mcp.WithMIMEType("application/json"))
}

func (s *Server) handleSpaceResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	id, err := trimPrefixUUID(req.Params.URI, "memory://spaces/")
	if err != nil {
		return nil, err
	}
	space, err := s.spaces.GetSpace(ctx, id)
	if err != nil {
		return nil, err
	}
	text, err := jsonMarshalText(space)
	if err != nil {
		return nil, err
	}
	return []mcp.ResourceContents{mcp.TextResourceContents{
		URI:      req.Params.URI,
		MIMEType: "application/json",
		Text:     text,
	}}, nil
}

func (s *Server) handleSessionResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	id, err := trimPrefixUUID(req.Params.URI, "memory://sessions/")
	if err != nil {
		return nil, err
	}
	sess, err := s.session.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	text, err := jsonMarshalText(sess)
	if err != nil {
		return nil, err
	}
	return []mcp.ResourceContents{mcp.TextResourceContents{
		URI:      req.Params.URI,
		MIMEType: "application/json",
		Text:     text,
	}}, nil
}

func (s *Server) handleStatsResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	stats, err := s.lifecycle.GetStats(ctx, "global")
	if err != nil {
		return nil, err
	}
	text, err := jsonMarshalText(stats)
	if err != nil {
		return nil, err
	}
	return []mcp.ResourceContents{mcp.TextResourceContents{
		URI:      uriStats,
		MIMEType: "application/json",
		Text:     text,
	}}, nil
}
