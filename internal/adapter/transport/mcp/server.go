package mcp

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// Server hosts the mneme MCP transport over the Step 7 application services.
type Server struct {
	mcp       *server.MCPServer
	memories  port.MemoryService
	recall    port.RecallService
	session   port.SessionService
	spaces    port.SpaceService
	lifecycle port.LifecycleService

	// sessions maps connection principal → bound session (§2.3).
	sessions *sessionRegistry

	// links optionally resolves get_memory include_links; nil yields [].
	links func(ctx context.Context, memoryID uuid.UUID) ([]any, error)
	// findProposal optionally resolves proposal → space for approve/reject.
	findProposal func(ctx context.Context, proposalID uuid.UUID) (uuid.UUID, bool)
}

// Option configures optional Server capabilities.
type Option func(*Server)

// WithLinkResolver supplies a resolver for get_memory include_links=true.
func WithLinkResolver(fn func(ctx context.Context, memoryID uuid.UUID) ([]any, error)) Option {
	return func(s *Server) { s.links = fn }
}

// WithProposalFinder supplies a proposal → space resolver used when
// approve_proposal/reject_proposal omit space_id.
func WithProposalFinder(fn func(ctx context.Context, proposalID uuid.UUID) (uuid.UUID, bool)) Option {
	return func(s *Server) { s.findProposal = fn }
}

// ActorOrAgent returns the principal identifier used for space listing.
func (p Principal) ActorOrAgent() string {
	if p.Actor != "" {
		return p.Actor
	}
	return p.AgentID
}

// NewServer builds the mneme MCP server: capabilities, 24 tools (§2.1–§2.5),
// and 3 resources (§2.6).
func NewServer(deps Services, opts ...Option) *Server {
	s := &Server{
		memories:  deps.Memories,
		recall:    deps.Recall,
		session:   deps.Sessions,
		spaces:    deps.Spaces,
		lifecycle: deps.Lifecycle,
		sessions:  newSessionRegistry(),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.mcp = server.NewMCPServer("mneme", "0.1.0",
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true), // list, subscribe
	)
	s.registerTools()
	s.registerResources()
	return s
}

// Services are the Step 7 application service ports the transport binds to.
type Services struct {
	Memories  port.MemoryService
	Recall    port.RecallService
	Sessions  port.SessionService
	Spaces    port.SpaceService
	Lifecycle port.LifecycleService
}

// ToolNames lists every registered tool name (contract: 24).
var ToolNames = []string{
	// memory (§2.1)
	"save_memory", "get_memory", "list_memories", "update_memory", "delete_memory", "link_memories",
	// recall (§2.2)
	"recall", "recall_async",
	// session (§2.3)
	"start_session", "get_session", "activate_memory", "deactivate_memory", "end_session",
	// space (§2.4)
	"create_space", "list_spaces", "get_space", "promote_memory",
	"review_proposals", "approve_proposal", "reject_proposal", "sync_space",
	// lifecycle (§2.5)
	"consolidate", "decay", "memory_stats",
}

type toolBinding struct {
	tool    mcp.Tool
	handler server.ToolHandlerFunc
}

func (s *Server) toolBindings() []toolBinding {
	return []toolBinding{
		{saveMemoryTool(), s.handleSaveMemory},
		{getMemoryTool(), s.handleGetMemory},
		{listMemoriesTool(), s.handleListMemories},
		{updateMemoryTool(), s.handleUpdateMemory},
		{deleteMemoryTool(), s.handleDeleteMemory},
		{linkMemoriesTool(), s.handleLinkMemories},

		{recallTool(), s.handleRecall},
		{recallAsyncTool(), s.handleRecallAsync},

		{startSessionTool(), s.handleStartSession},
		{getSessionTool(), s.handleGetSession},
		{activateMemoryTool(), s.handleActivateMemory},
		{deactivateMemoryTool(), s.handleDeactivateMemory},
		{endSessionTool(), s.handleEndSession},

		{createSpaceTool(), s.handleCreateSpace},
		{listSpacesTool(), s.handleListSpaces},
		{getSpaceTool(), s.handleGetSpace},
		{promoteMemoryTool(), s.handlePromoteMemory},
		{reviewProposalsTool(), s.handleReviewProposals},
		{approveProposalTool(), s.handleApproveProposal},
		{rejectProposalTool(), s.handleRejectProposal},
		{syncSpaceTool(), s.handleSyncSpace},

		{consolidateTool(), s.handleConsolidate},
		{decayTool(), s.handleDecay},
		{memoryStatsTool(), s.handleMemoryStats},
	}
}

func (s *Server) registerTools() {
	for _, b := range s.toolBindings() {
		s.mcp.AddTool(b.tool, b.handler)
	}
}

func (s *Server) registerResources() {
	s.mcp.AddResourceTemplate(spaceResource(), s.handleSpaceResource)
	s.mcp.AddResourceTemplate(sessionResource(), s.handleSessionResource)
	s.mcp.AddResource(statsResource(), s.handleStatsResource)
}

// MCPServer exposes the underlying mcp-go server for transports.
func (s *Server) MCPServer() *server.MCPServer { return s.mcp }

// ServeStdio runs the server over stdio (default for local agent processes).
func (s *Server) ServeStdio() error {
	return server.ServeStdio(s.mcp)
}

// ServeHTTP runs the server over streamable-http for remote clients. The
// connection principal is resolved per request from the x-principal-* /
// OAuth headers (§2.0) and stamped into the tool context.
func (s *Server) ServeHTTP(addr string) error {
	httpServer := server.NewStreamableHTTPServer(s.mcp,
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			p := Principal{Type: principalTypeOf(r)}
			if v := r.Header.Get("X-Principal-Agent-Id"); v != "" {
				p.AgentID = v
			}
			if v := r.Header.Get("X-Principal-Actor"); v != "" {
				p.Actor = v
			}
			if v := r.Header.Get("X-Principal-User-Id"); v != "" {
				p.UserID = v
			}
			if p.AgentID == "" && p.Actor == "" && p.UserID == "" {
				return ctx // anonymous: per-call defaults apply
			}
			return WithConnectionPrincipal(ctx, p)
		}))
	mux := http.NewServeMux()
	mux.Handle("/mcp", httpServer)
	return http.ListenAndServe(addr, mux)
}

func principalTypeOf(r *http.Request) domain.PrincipalType {
	if v := r.Header.Get("X-Principal-Type"); v != "" {
		if t, err := domain.ParsePrincipalType(v); err == nil {
			return t
		}
	}
	return domain.PrincipalAgent
}
