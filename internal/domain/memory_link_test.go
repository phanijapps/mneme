package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryLinkValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*MemoryLink)
		wantErr bool
	}{
		{"valid", func(*MemoryLink) {}, false},
		{"weight zero ok", func(l *MemoryLink) { l.Weight = 0 }, false},
		{"weight one ok", func(l *MemoryLink) { l.Weight = 1 }, false},
		{"weight above 1", func(l *MemoryLink) { l.Weight = 1.01 }, true},
		{"weight negative", func(l *MemoryLink) { l.Weight = -0.1 }, true},
		{"invalid relationship", func(l *MemoryLink) {
			l.RelationshipType = RelationshipType("parent_of")
		}, true},
		{"self link", func(l *MemoryLink) { l.TargetID = l.SourceID }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &MemoryLink{
				ID:               uuid.New(),
				SourceID:         uuid.New(),
				TargetID:         uuid.New(),
				RelationshipType: RelationshipDerivedFrom,
				Weight:           0.91,
				CreatedAt:        time.Now(),
			}
			tt.mutate(l)
			err := l.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, CodeValidationErr, err.(*Error).Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
