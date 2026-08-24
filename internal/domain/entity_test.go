package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntityValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Entity)
		wantErr bool
	}{
		{"valid", func(*Entity) {}, false},
		{"missing name", func(e *Entity) { e.Name = "" }, true},
		{"invalid entity_type", func(e *Entity) { e.EntityType = EntityType("place") }, true},
		{"valid with fact", func(e *Entity) {
			e.Facts = []EntityFact{{ID: uuid.New(), EntityID: e.EntityID, Fact: "works on mneme",
				ValidFrom: time.Now()}}
		}, false},
		{"fact inverted range", func(e *Entity) {
			from := time.Now()
			e.Facts = []EntityFact{{ID: uuid.New(), EntityID: e.EntityID, Fact: "x",
				ValidFrom: from, ValidUntil: ptrTime(from.Add(-time.Hour))}}
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Entity{EntityID: uuid.New(), Name: "pgvector", EntityType: EntityTool,
				CreatedAt: time.Now()}
			tt.mutate(e)
			err := e.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, CodeValidationErr, err.(*Error).Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
