package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/phanijapps/mneme/internal/port"
)

// Server wraps the chi router in an *http.Server with graceful shutdown.
type Server struct {
	http *http.Server
}

// ServerDeps carries the five service ports the transport calls.
type ServerDeps struct {
	Memories  port.MemoryService
	Recall    port.RecallService
	Sessions  port.SessionService
	Spaces    port.SpaceService
	Lifecycle port.LifecycleService
}

// NewServer builds the router from deps and returns a ready *http.Server
// wrapper. Call Start to listen.
func NewServer(deps ServerDeps) *Server {
	handler := NewRouter(NewHandlers(deps.Memories, deps.Recall, deps.Sessions, deps.Spaces, deps.Lifecycle))
	return &Server{http: &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}}
}

// Start begins listening on addr and blocks until the listener returns.
func (s *Server) Start(addr string) error {
	s.http.Addr = addr
	err := s.http.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown drains in-flight requests with a deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// Handler exposes the underlying http.Handler (tests).
func (s *Server) Handler() http.Handler { return s.http.Handler }
