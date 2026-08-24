// Package domain defines the core entities, enums, and error types of the
// mneme memory system. It is pure Go: no adapter imports (validator struct
// tags and the uuid/pgvector value types required by the data model are the
// only concessions).
package domain

import (
	"fmt"
	"slices"
)

// enumValue is the constraint for all typed string enums in this package.
type enumValue interface {
	~string
}

// parseEnum returns the typed value matching raw, or an error naming the type.
func parseEnum[T enumValue](valid []T, raw string) (T, error) {
	for _, v := range valid {
		if string(v) == raw {
			return v, nil
		}
	}
	var zero T
	return zero, fmt.Errorf("invalid %T %q (valid: %v)", zero, raw, valid)
}

// containsEnum reports whether v is one of the declared constants.
func containsEnum[T enumValue](valid []T, v T) bool {
	return slices.Contains(valid, v)
}

// MemoryType classifies a Memory: episodic, semantic, or procedural.
type MemoryType string

const (
	MemoryTypeEpisodic   MemoryType = "episodic"
	MemoryTypeSemantic   MemoryType = "semantic"
	MemoryTypeProcedural MemoryType = "procedural"
)

var memoryTypeValues = []MemoryType{MemoryTypeEpisodic, MemoryTypeSemantic, MemoryTypeProcedural}

// ParseMemoryType converts a string into a MemoryType.
func ParseMemoryType(raw string) (MemoryType, error) { return parseEnum(memoryTypeValues, raw) }

// String implements fmt.Stringer.
func (t MemoryType) String() string { return string(t) }

// Valid reports whether the value is a declared constant.
func (t MemoryType) Valid() bool { return containsEnum(memoryTypeValues, t) }

// ContentFormat is the encoding of Memory.Content.
type ContentFormat string

const (
	ContentFormatMarkdown ContentFormat = "markdown"
	ContentFormatPlain    ContentFormat = "plain"
	ContentFormatJSON     ContentFormat = "json"
)

var contentFormatValues = []ContentFormat{ContentFormatMarkdown, ContentFormatPlain, ContentFormatJSON}

// ParseContentFormat converts a string into a ContentFormat.
func ParseContentFormat(raw string) (ContentFormat, error) {
	return parseEnum(contentFormatValues, raw)
}

// String implements fmt.Stringer.
func (f ContentFormat) String() string { return string(f) }

// Valid reports whether the value is a declared constant.
func (f ContentFormat) Valid() bool { return containsEnum(contentFormatValues, f) }

// Origin records how a Memory came to exist (provenance, client-asserted).
type Origin string

const (
	OriginAgentObservation Origin = "agent_observation"
	OriginUserInstruction  Origin = "user_instruction"
	OriginFileArtifact     Origin = "file_artifact"
	OriginConsolidation    Origin = "consolidation"
)

var originValues = []Origin{OriginAgentObservation, OriginUserInstruction, OriginFileArtifact, OriginConsolidation}

// ParseOrigin converts a string into an Origin.
func ParseOrigin(raw string) (Origin, error) { return parseEnum(originValues, raw) }

// String implements fmt.Stringer.
func (o Origin) String() string { return string(o) }

// Valid reports whether the value is a declared constant.
func (o Origin) Valid() bool { return containsEnum(originValues, o) }

// AccessScope separates agent-written individual memory from shared memory.
// Values follow pgvector-data-model §3.3 (individual | shared).
type AccessScope string

const (
	AccessScopeIndividual AccessScope = "individual"
	AccessScopeShared     AccessScope = "shared"
)

var accessScopeValues = []AccessScope{AccessScopeIndividual, AccessScopeShared}

// ParseAccessScope converts a string into an AccessScope.
func ParseAccessScope(raw string) (AccessScope, error) { return parseEnum(accessScopeValues, raw) }

// String implements fmt.Stringer.
func (s AccessScope) String() string { return string(s) }

// Valid reports whether the value is a declared constant.
func (s AccessScope) Valid() bool { return containsEnum(accessScopeValues, s) }

// RelationshipType is the edge family of a MemoryLink.
type RelationshipType string

const (
	RelationshipDerivedFrom   RelationshipType = "derived_from"
	RelationshipSupersedes    RelationshipType = "supersedes"
	RelationshipSimilarTo     RelationshipType = "similar_to"
	RelationshipCoOccursWith  RelationshipType = "co_occurs_with"
	RelationshipCausalNext    RelationshipType = "causal_next"
	RelationshipAnchorsEntity RelationshipType = "anchors_entity"
)

var relationshipTypeValues = []RelationshipType{
	RelationshipDerivedFrom, RelationshipSupersedes, RelationshipSimilarTo,
	RelationshipCoOccursWith, RelationshipCausalNext, RelationshipAnchorsEntity,
}

// ParseRelationshipType converts a string into a RelationshipType.
func ParseRelationshipType(raw string) (RelationshipType, error) {
	return parseEnum(relationshipTypeValues, raw)
}

// String implements fmt.Stringer.
func (r RelationshipType) String() string { return string(r) }

// Valid reports whether the value is a declared constant.
func (r RelationshipType) Valid() bool { return containsEnum(relationshipTypeValues, r) }

// EntityType classifies an Entity in the entity registry.
type EntityType string

const (
	EntityPerson       EntityType = "person"
	EntityProject      EntityType = "project"
	EntityRepository   EntityType = "repository"
	EntityTool         EntityType = "tool"
	EntityOrganization EntityType = "organization"
	EntityConcept      EntityType = "concept"
)

var entityTypeValues = []EntityType{
	EntityPerson, EntityProject, EntityRepository, EntityTool, EntityOrganization, EntityConcept,
}

// ParseEntityType converts a string into an EntityType.
func ParseEntityType(raw string) (EntityType, error) { return parseEnum(entityTypeValues, raw) }

// String implements fmt.Stringer.
func (t EntityType) String() string { return string(t) }

// Valid reports whether the value is a declared constant.
func (t EntityType) Valid() bool { return containsEnum(entityTypeValues, t) }

// PrincipalType is the kind of access-control subject.
type PrincipalType string

const (
	PrincipalUser    PrincipalType = "user"
	PrincipalAgent   PrincipalType = "agent"
	PrincipalSession PrincipalType = "session"
	PrincipalGroup   PrincipalType = "group"
)

var principalTypeValues = []PrincipalType{PrincipalUser, PrincipalAgent, PrincipalSession, PrincipalGroup}

// ParsePrincipalType converts a string into a PrincipalType.
func ParsePrincipalType(raw string) (PrincipalType, error) {
	return parseEnum(principalTypeValues, raw)
}

// String implements fmt.Stringer.
func (p PrincipalType) String() string { return string(p) }

// Valid reports whether the value is a declared constant.
func (p PrincipalType) Valid() bool { return containsEnum(principalTypeValues, p) }

// SpaceOwnerType is who owns a SharedMemorySpace.
type SpaceOwnerType string

const (
	SpaceOwnerUser         SpaceOwnerType = "user"
	SpaceOwnerAgent        SpaceOwnerType = "agent"
	SpaceOwnerTeam         SpaceOwnerType = "team"
	SpaceOwnerOrganization SpaceOwnerType = "organization"
)

var spaceOwnerTypeValues = []SpaceOwnerType{SpaceOwnerUser, SpaceOwnerAgent, SpaceOwnerTeam, SpaceOwnerOrganization}

// ParseSpaceOwnerType converts a string into a SpaceOwnerType.
func ParseSpaceOwnerType(raw string) (SpaceOwnerType, error) {
	return parseEnum(spaceOwnerTypeValues, raw)
}

// String implements fmt.Stringer.
func (o SpaceOwnerType) String() string { return string(o) }

// Valid reports whether the value is a declared constant.
func (o SpaceOwnerType) Valid() bool { return containsEnum(spaceOwnerTypeValues, o) }

// AgentType identifies the agent runtime a session belongs to.
type AgentType string

const (
	AgentClaudeCode AgentType = "claude-code"
	AgentCodex      AgentType = "codex"
	AgentCursor     AgentType = "cursor"
	AgentLetta      AgentType = "letta"
	AgentCustom     AgentType = "custom"
)

var agentTypeValues = []AgentType{AgentClaudeCode, AgentCodex, AgentCursor, AgentLetta, AgentCustom}

// ParseAgentType converts a string into an AgentType.
func ParseAgentType(raw string) (AgentType, error) { return parseEnum(agentTypeValues, raw) }

// String implements fmt.Stringer.
func (a AgentType) String() string { return string(a) }

// Valid reports whether the value is a declared constant.
func (a AgentType) Valid() bool { return containsEnum(agentTypeValues, a) }

// TriggerType is the recall trigger class.
type TriggerType string

const (
	TriggerTaskContext  TriggerType = "task_context"
	TriggerUserQuery    TriggerType = "user_query"
	TriggerTemporal     TriggerType = "temporal"
	TriggerAssociative  TriggerType = "associative"
	TriggerSessionStart TriggerType = "session_start"
)

var triggerTypeValues = []TriggerType{
	TriggerTaskContext, TriggerUserQuery, TriggerTemporal, TriggerAssociative, TriggerSessionStart,
}

// ParseTriggerType converts a string into a TriggerType.
func ParseTriggerType(raw string) (TriggerType, error) { return parseEnum(triggerTypeValues, raw) }

// String implements fmt.Stringer.
func (t TriggerType) String() string { return string(t) }

// Valid reports whether the value is a declared constant.
func (t TriggerType) Valid() bool { return containsEnum(triggerTypeValues, t) }

// StrategyType selects a parallel retrieval path (bm25 is the sparse path).
type StrategyType string

const (
	StrategyVector   StrategyType = "vector"
	StrategyBM25     StrategyType = "bm25"
	StrategyGraph    StrategyType = "graph"
	StrategyTemporal StrategyType = "temporal"
)

var strategyTypeValues = []StrategyType{StrategyVector, StrategyBM25, StrategyGraph, StrategyTemporal}

// ParseStrategyType converts a string into a StrategyType.
func ParseStrategyType(raw string) (StrategyType, error) { return parseEnum(strategyTypeValues, raw) }

// String implements fmt.Stringer.
func (s StrategyType) String() string { return string(s) }

// Valid reports whether the value is a declared constant.
func (s StrategyType) Valid() bool { return containsEnum(strategyTypeValues, s) }

// RerankType is the post-merge re-ranking strategy.
type RerankType string

const (
	RerankCrossEncoder RerankType = "cross_encoder"
	RerankNone         RerankType = "none"
)

var rerankTypeValues = []RerankType{RerankCrossEncoder, RerankNone}

// ParseRerankType converts a string into a RerankType.
func ParseRerankType(raw string) (RerankType, error) { return parseEnum(rerankTypeValues, raw) }

// String implements fmt.Stringer.
func (r RerankType) String() string { return string(r) }

// Valid reports whether the value is a declared constant.
func (r RerankType) Valid() bool { return containsEnum(rerankTypeValues, r) }

// RecallMode is sync (blocking) or async (polled) execution.
type RecallMode string

const (
	RecallModeSync  RecallMode = "sync"
	RecallModeAsync RecallMode = "async"
)

var recallModeValues = []RecallMode{RecallModeSync, RecallModeAsync}

// ParseRecallMode converts a string into a RecallMode.
func ParseRecallMode(raw string) (RecallMode, error) { return parseEnum(recallModeValues, raw) }

// String implements fmt.Stringer.
func (m RecallMode) String() string { return string(m) }

// Valid reports whether the value is a declared constant.
func (m RecallMode) Valid() bool { return containsEnum(recallModeValues, m) }

// RecallStatus is the async lifecycle of a recall request (review F3).
type RecallStatus string

const (
	RecallStatusQueued    RecallStatus = "queued"
	RecallStatusRunning   RecallStatus = "running"
	RecallStatusCompleted RecallStatus = "completed"
	RecallStatusFailed    RecallStatus = "failed"
)

var recallStatusValues = []RecallStatus{
	RecallStatusQueued, RecallStatusRunning, RecallStatusCompleted, RecallStatusFailed,
}

// ParseRecallStatus converts a string into a RecallStatus.
func ParseRecallStatus(raw string) (RecallStatus, error) { return parseEnum(recallStatusValues, raw) }

// String implements fmt.Stringer.
func (s RecallStatus) String() string { return string(s) }

// Valid reports whether the value is a declared constant.
func (s RecallStatus) Valid() bool { return containsEnum(recallStatusValues, s) }

// JobKind is the class of a lifecycle job.
type JobKind string

const (
	JobConsolidation JobKind = "consolidation"
	JobDecay         JobKind = "decay"
	JobSpaceSync     JobKind = "space_sync"
	JobSessionEnd    JobKind = "session_end"
)

var jobKindValues = []JobKind{JobConsolidation, JobDecay, JobSpaceSync, JobSessionEnd}

// ParseJobKind converts a string into a JobKind.
func ParseJobKind(raw string) (JobKind, error) { return parseEnum(jobKindValues, raw) }

// String implements fmt.Stringer.
func (k JobKind) String() string { return string(k) }

// Valid reports whether the value is a declared constant.
func (k JobKind) Valid() bool { return containsEnum(jobKindValues, k) }

// JobStatus is the lifecycle state of a job (or recall request).
type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

var jobStatusValues = []JobStatus{JobStatusQueued, JobStatusRunning, JobStatusCompleted, JobStatusFailed}

// ParseJobStatus converts a string into a JobStatus.
func ParseJobStatus(raw string) (JobStatus, error) { return parseEnum(jobStatusValues, raw) }

// String implements fmt.Stringer.
func (s JobStatus) String() string { return string(s) }

// Valid reports whether the value is a declared constant.
func (s JobStatus) Valid() bool { return containsEnum(jobStatusValues, s) }

// ProposalStatus is the human-review lifecycle of a PromotionProposal.
type ProposalStatus string

const (
	ProposalStatusDraft    ProposalStatus = "draft"
	ProposalStatusInReview ProposalStatus = "in_review"
	ProposalStatusMerged   ProposalStatus = "merged"
	ProposalStatusRejected ProposalStatus = "rejected"
)

var proposalStatusValues = []ProposalStatus{
	ProposalStatusDraft, ProposalStatusInReview, ProposalStatusMerged, ProposalStatusRejected,
}

// ParseProposalStatus converts a string into a ProposalStatus.
func ParseProposalStatus(raw string) (ProposalStatus, error) {
	return parseEnum(proposalStatusValues, raw)
}

// String implements fmt.Stringer.
func (s ProposalStatus) String() string { return string(s) }

// Valid reports whether the value is a declared constant.
func (s ProposalStatus) Valid() bool { return containsEnum(proposalStatusValues, s) }

// Terminal reports whether the status can no longer change.
func (s ProposalStatus) Terminal() bool {
	return s == ProposalStatusMerged || s == ProposalStatusRejected
}

// ProposalTargetKind is the artifact class a proposal writes into.
type ProposalTargetKind string

const (
	ProposalTargetSpec      ProposalTargetKind = "spec"
	ProposalTargetRule      ProposalTargetKind = "rule"
	ProposalTargetAgentDoc  ProposalTargetKind = "agent_doc"
	ProposalTargetMemoryDoc ProposalTargetKind = "memory_doc"
)

var proposalTargetKindValues = []ProposalTargetKind{
	ProposalTargetSpec, ProposalTargetRule, ProposalTargetAgentDoc, ProposalTargetMemoryDoc,
}

// ParseProposalTargetKind converts a string into a ProposalTargetKind.
func ParseProposalTargetKind(raw string) (ProposalTargetKind, error) {
	return parseEnum(proposalTargetKindValues, raw)
}

// String implements fmt.Stringer.
func (k ProposalTargetKind) String() string { return string(k) }

// Valid reports whether the value is a declared constant.
func (k ProposalTargetKind) Valid() bool { return containsEnum(proposalTargetKindValues, k) }

// ProposalTargetRole is the memory role the promoted artifact plays.
type ProposalTargetRole string

const (
	ProposalRoleProcedural ProposalTargetRole = "procedural"
	ProposalRoleSemantic   ProposalTargetRole = "semantic"
	ProposalRoleEpisodic   ProposalTargetRole = "episodic"
)

var proposalTargetRoleValues = []ProposalTargetRole{
	ProposalRoleProcedural, ProposalRoleSemantic, ProposalRoleEpisodic,
}

// ParseProposalTargetRole converts a string into a ProposalTargetRole.
func ParseProposalTargetRole(raw string) (ProposalTargetRole, error) {
	return parseEnum(proposalTargetRoleValues, raw)
}

// String implements fmt.Stringer.
func (r ProposalTargetRole) String() string { return string(r) }

// Valid reports whether the value is a declared constant.
func (r ProposalTargetRole) Valid() bool { return containsEnum(proposalTargetRoleValues, r) }

// SyncStatus is the backend-neutral replication state of a space.
type SyncStatus string

const (
	SyncStatusInSync        SyncStatus = "in_sync"
	SyncStatusPendingReview SyncStatus = "pending_review"
	SyncStatusDiverged      SyncStatus = "diverged"
	SyncStatusOffline       SyncStatus = "offline"
)

var syncStatusValues = []SyncStatus{SyncStatusInSync, SyncStatusPendingReview, SyncStatusDiverged, SyncStatusOffline}

// ParseSyncStatus converts a string into a SyncStatus.
func ParseSyncStatus(raw string) (SyncStatus, error) { return parseEnum(syncStatusValues, raw) }

// String implements fmt.Stringer.
func (s SyncStatus) String() string { return string(s) }

// Valid reports whether the value is a declared constant.
func (s SyncStatus) Valid() bool { return containsEnum(syncStatusValues, s) }

// StorageBackendType is the class of backend realizing a space.
// Values follow pgvector-data-model §3.1 (files | relational | vector | graph | hybrid).
type StorageBackendType string

const (
	BackendFiles      StorageBackendType = "files"
	BackendRelational StorageBackendType = "relational"
	BackendVector     StorageBackendType = "vector"
	BackendGraph      StorageBackendType = "graph"
	BackendHybrid     StorageBackendType = "hybrid"
)

var storageBackendTypeValues = []StorageBackendType{
	BackendFiles, BackendRelational, BackendVector, BackendGraph, BackendHybrid,
}

// ParseStorageBackendType converts a string into a StorageBackendType.
func ParseStorageBackendType(raw string) (StorageBackendType, error) {
	return parseEnum(storageBackendTypeValues, raw)
}

// String implements fmt.Stringer.
func (b StorageBackendType) String() string { return string(b) }

// Valid reports whether the value is a declared constant.
func (b StorageBackendType) Valid() bool { return containsEnum(storageBackendTypeValues, b) }

// DefaultAccess is what an unlisted principal gets on a space.
type DefaultAccess string

const (
	DefaultAccessRead  DefaultAccess = "read"
	DefaultAccessWrite DefaultAccess = "write"
	DefaultAccessNone  DefaultAccess = "none"
)

var defaultAccessValues = []DefaultAccess{DefaultAccessRead, DefaultAccessWrite, DefaultAccessNone}

// ParseDefaultAccess converts a string into a DefaultAccess.
func ParseDefaultAccess(raw string) (DefaultAccess, error) {
	return parseEnum(defaultAccessValues, raw)
}

// String implements fmt.Stringer.
func (a DefaultAccess) String() string { return string(a) }

// Valid reports whether the value is a declared constant.
func (a DefaultAccess) Valid() bool { return containsEnum(defaultAccessValues, a) }

// WritePolicy is who may write to a space.
type WritePolicy string

const (
	WriteOwnerApproved   WritePolicy = "owner_approved"
	WriteParticipantFree WritePolicy = "participant_free"
	WriteProposalOnly    WritePolicy = "proposal_only"
)

var writePolicyValues = []WritePolicy{WriteOwnerApproved, WriteParticipantFree, WriteProposalOnly}

// ParseWritePolicy converts a string into a WritePolicy.
func ParseWritePolicy(raw string) (WritePolicy, error) { return parseEnum(writePolicyValues, raw) }

// String implements fmt.Stringer.
func (w WritePolicy) String() string { return string(w) }

// Valid reports whether the value is a declared constant.
func (w WritePolicy) Valid() bool { return containsEnum(writePolicyValues, w) }

// PromotePolicy is whether promotion from individual memory is reviewed.
type PromotePolicy string

const (
	PromoteHumanReview PromotePolicy = "human_review"
	PromoteAuto        PromotePolicy = "auto"
)

var promotePolicyValues = []PromotePolicy{PromoteHumanReview, PromoteAuto}

// ParsePromotePolicy converts a string into a PromotePolicy.
func ParsePromotePolicy(raw string) (PromotePolicy, error) {
	return parseEnum(promotePolicyValues, raw)
}

// String implements fmt.Stringer.
func (p PromotePolicy) String() string { return string(p) }

// Valid reports whether the value is a declared constant.
func (p PromotePolicy) Valid() bool { return containsEnum(promotePolicyValues, p) }

// AccessLevel is a principal's access level on a space.
type AccessLevel string

const (
	AccessLevelRead    AccessLevel = "read"
	AccessLevelWrite   AccessLevel = "write"
	AccessLevelPromote AccessLevel = "promote"
	AccessLevelAdmin   AccessLevel = "admin"
)

var accessLevelValues = []AccessLevel{AccessLevelRead, AccessLevelWrite, AccessLevelPromote, AccessLevelAdmin}

// ParseAccessLevel converts a string into an AccessLevel.
func ParseAccessLevel(raw string) (AccessLevel, error) { return parseEnum(accessLevelValues, raw) }

// String implements fmt.Stringer.
func (l AccessLevel) String() string { return string(l) }

// Valid reports whether the value is a declared constant.
func (l AccessLevel) Valid() bool { return containsEnum(accessLevelValues, l) }

// AccessType is how a memory was accessed (memory_access_log, review F8).
type AccessType string

const (
	AccessTypeRecall          AccessType = "recall"
	AccessTypeDirectGet       AccessType = "direct_get"
	AccessTypeSessionActivate AccessType = "session_activate"
)

var accessTypeValues = []AccessType{AccessTypeRecall, AccessTypeDirectGet, AccessTypeSessionActivate}

// ParseAccessType converts a string into an AccessType.
func ParseAccessType(raw string) (AccessType, error) { return parseEnum(accessTypeValues, raw) }

// String implements fmt.Stringer.
func (a AccessType) String() string { return string(a) }

// Valid reports whether the value is a declared constant.
func (a AccessType) Valid() bool { return containsEnum(accessTypeValues, a) }

// DistanceMetric is the vector distance metric of an embedding model.
type DistanceMetric string

const (
	MetricCosine DistanceMetric = "cosine"
	MetricL2     DistanceMetric = "l2"
	MetricIP     DistanceMetric = "ip"
)

var distanceMetricValues = []DistanceMetric{MetricCosine, MetricL2, MetricIP}

// ParseDistanceMetric converts a string into a DistanceMetric.
func ParseDistanceMetric(raw string) (DistanceMetric, error) {
	return parseEnum(distanceMetricValues, raw)
}

// String implements fmt.Stringer.
func (m DistanceMetric) String() string { return string(m) }

// Valid reports whether the value is a declared constant.
func (m DistanceMetric) Valid() bool { return containsEnum(distanceMetricValues, m) }
