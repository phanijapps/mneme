package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/phanijapps/mneme/internal/port"
)

// NewRouter builds the full chi router: global middleware chain, health,
// swagger, and the authenticated /api/v1 surface (26 endpoints).
func NewRouter(h *Handlers) http.Handler {
	r := chi.NewRouter()

	r.Use(RequestIDMiddleware)
	r.Use(ErrorMiddleware)
	r.Use(CORSMiddleware)
	r.Use(LoggingMiddleware(nil))
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/swagger/*", httpSwagger.WrapHandler)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(AuthMiddleware)

		// Memories
		r.Post("/memories", h.CreateMemory)
		r.Get("/memories", h.ListMemories)
		r.Get("/memories/{id}", h.GetMemory)
		r.Put("/memories/{id}", h.UpdateMemory)
		r.Delete("/memories/{id}", h.DeleteMemory)
		r.Post("/memories/{id}/links", h.CreateLink)
		r.Get("/memories/{id}/links", h.GetLinks)

		// Recall
		r.Post("/recall", h.RecallSubmit)
		r.Get("/recall/{request_id}", h.GetRecallStatus)

		// Sessions
		r.Post("/sessions", h.CreateSession)
		r.Get("/sessions/{session_id}", h.GetSession)
		r.Post("/sessions/{session_id}/memories", h.ActivateMemory)
		r.Delete("/sessions/{session_id}/memories/{memory_id}", h.DeactivateMemory)
		r.Post("/sessions/{session_id}/end", h.EndSession)

		// Spaces
		r.Post("/spaces", h.CreateSpace)
		r.Get("/spaces", h.ListSpaces)
		r.Get("/spaces/{space_id}", h.GetSpace)
		r.Put("/spaces/{space_id}", h.UpdateSpace)
		r.Post("/spaces/{space_id}/memories", h.PromoteMemory)
		r.Get("/spaces/{space_id}/proposals", h.ListProposals)
		r.Post("/spaces/{space_id}/proposals/{proposal_id}/approve", h.ApproveProposal)
		r.Post("/spaces/{space_id}/proposals/{proposal_id}/reject", h.RejectProposal)
		r.Post("/spaces/{space_id}/sync", h.SyncSpace)

		// Lifecycle
		r.Post("/lifecycle/consolidate", h.Consolidate)
		r.Post("/lifecycle/decay", h.Decay)
		r.Get("/lifecycle/jobs/{job_id}", h.GetJob)
		r.Get("/lifecycle/stats", h.GetStats)
	})

	return r
}

// NewRouterFromServices is the wiring convenience used by main and tests.
func NewRouterFromServices(m port.MemoryService, rc port.RecallService, s port.SessionService, sp port.SpaceService, l port.LifecycleService) http.Handler {
	return NewRouter(NewHandlers(m, rc, s, sp, l))
}
