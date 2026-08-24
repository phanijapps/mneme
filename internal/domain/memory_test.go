package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validMemory() *Memory {
	now := time.Now().UTC()
	return &Memory{
		ID:                 uuid.New(),
		Type:               MemoryTypeSemantic,
		Content:            "Use pgvector HNSW indexes for dense recall.",
		ContentFormat:      ContentFormatMarkdown,
		Tags:               []string{"postgres", "retrieval"},
		Origin:             OriginAgentObservation,
		OwnerPrincipalType: PrincipalUser,
		OwnerPrincipalID:   "user-123",
		CreatedAt:          now,
		UpdatedAt:          now,
		Confidence:         ptrFloat(0.9),
		DecayScore:         ptrFloat(1.0),
		ValidFrom:          &now,
		Version:            1,
		AccessScope:        AccessScopeIndividual,
	}
}

func ptrFloat(f float64) *float64    { return &f }
func ptrTime(t time.Time) *time.Time { return &t }

func TestMemoryValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Memory)
		wantErr bool
		field   string
	}{
		{"valid", func(*Memory) {}, false, ""},
		{"missing content", func(m *Memory) { m.Content = "" }, true, "Memory.Content"},
		{"blank content", func(m *Memory) { m.Content = "   " }, true, "Memory.Content"}, // btrim CHECK
		{"invalid type", func(m *Memory) { m.Type = MemoryType("flashback") }, true, "Memory.Type"},
		{"invalid format", func(m *Memory) { m.ContentFormat = ContentFormat("html") }, true, "Memory.ContentFormat"},
		{"invalid origin", func(m *Memory) { m.Origin = Origin("divine") }, true, "Memory.Origin"},
		{"scope shared without space", func(m *Memory) { m.AccessScope = AccessScopeShared }, true, "Memory.SharedSpaceID"},
		{"scope individual with space", func(m *Memory) { m.SharedSpaceID = ptrUUID() }, true, "Memory.SharedSpaceID"},
		{"scope shared with space", func(m *Memory) {
			m.AccessScope = AccessScopeShared
			m.SharedSpaceID = ptrUUID()
		}, false, ""},
		{"confidence above 1", func(m *Memory) { m.Confidence = ptrFloat(1.5) }, true, "Memory.Confidence"},
		{"confidence negative", func(m *Memory) { m.Confidence = ptrFloat(-0.1) }, true, "Memory.Confidence"},
		{"decay above 1", func(m *Memory) { m.DecayScore = ptrFloat(1.01) }, true, "Memory.DecayScore"},
		{"version zero", func(m *Memory) { m.Version = 0 }, true, "Memory.Version"},
		{"missing owner id", func(m *Memory) { m.OwnerPrincipalID = "" }, true, "Memory.OwnerPrincipalID"},
		{"invalid owner type", func(m *Memory) { m.OwnerPrincipalType = PrincipalType("robot") }, true, "Memory.OwnerPrincipalType"},
		{"invalid tag", func(m *Memory) { m.Tags = []string{"Bad Tag"} }, true, "Memory.Tags"},
		{"tag with uppercase", func(m *Memory) { m.Tags = []string{"Postgres"} }, true, "Memory.Tags"},
		{"tag leading dash", func(m *Memory) { m.Tags = []string{"-bad"} }, true, "Memory.Tags"},
		{"valid underscore tag", func(m *Memory) { m.Tags = []string{"pg_vector", "v2"} }, false, ""},
		{"valid_until before valid_from", func(m *Memory) {
			m.ValidUntil = ptrTime(m.CreatedAt.Add(-time.Hour))
		}, true, "Memory.ValidUntil"},
		{"ttl before created", func(m *Memory) {
			m.TTLExpiresAt = ptrTime(m.CreatedAt.Add(-time.Minute))
		}, true, "Memory.TTLExpiresAt"},
		{"ttl after created", func(m *Memory) {
			m.TTLExpiresAt = ptrTime(m.CreatedAt.Add(24 * time.Hour))
		}, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validMemory()
			tt.mutate(m)
			err := m.Validate()
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			de, ok := err.(*Error)
			require.True(t, ok, "expected *domain.Error, got %T", err)
			assert.Equal(t, CodeValidationErr, de.Code)
			if tt.field != "" {
				assert.Equal(t, tt.field, de.Details["field"])
			}
		})
	}
}

func TestMemoryValidityRange(t *testing.T) {
	now := time.Now().UTC()
	later := now.Add(time.Hour)

	m := &Memory{ValidFrom: &now, ValidUntil: &later}
	iv := m.ValidityRange()
	require.NotNil(t, iv)
	assert.True(t, iv.Contains(now.Add(time.Minute)))
	assert.False(t, iv.Contains(later), "half-open interval excludes end")
	assert.False(t, iv.Contains(now.Add(-time.Minute)))

	// Open-ended: no ValidUntil means unbounded.
	m2 := &Memory{ValidFrom: &now}
	require.NotNil(t, m2.ValidityRange())
	assert.True(t, m2.ValidityRange().Contains(later))

	// Inverted range is invalid → nil.
	m3 := &Memory{ValidFrom: &later, ValidUntil: &now}
	assert.Nil(t, m3.ValidityRange())
}

func TestValidTagName(t *testing.T) {
	valid := []string{"a", "pg_vector", "v2", "a-b_c", "0day"}
	invalid := []string{"", "-a", "_a", "A", "a b", "a.b", "tag!"}
	for _, tag := range valid {
		assert.True(t, ValidTagName(tag), "%q should be valid", tag)
	}
	for _, tag := range invalid {
		assert.False(t, ValidTagName(tag), "%q should be invalid", tag)
	}
}

func ptrUUID() *uuid.UUID {
	id := uuid.New()
	return &id
}
