package http

// General API info consumed by swaggo when generating the OpenAPI spec
// served at /swagger/index.html.
//
//	@title			mneme API
//	@version		1.0
//	@description	AI Agent Memory & Recall System — supersession-first memory lifecycle with 4-way hybrid retrieval on PostgreSQL + pgvector.
//	@description	All /api/v1 endpoints require authentication via Authorization: Bearer or X-API-Key.
//
//	@host		localhost:8080
//	@BasePath	/api/v1
//
//	@securityDefinitions.apikey	ApiKeyAuth
//	@in							header
//	@name						Authorization
