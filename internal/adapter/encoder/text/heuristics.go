// Package text holds the content heuristics shared by encoder adapters:
// memory-type classification, entity extraction, and token estimation.
// They are deliberately cheap and deterministic — the embedding backend
// supplies the semantic signal; these heuristics only shape metadata.
package text

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/phanijapps/mneme/internal/domain"
)

// imperativeVerbs mark how-to content: procedural memory.
var imperativeVerbs = map[string]bool{
	"build": true, "create": true, "run": true, "test": true, "deploy": true,
	"install": true, "configure": true, "set": true, "use": true, "always": true,
	"never": true, "must": true, "should": true, "add": true, "remove": true,
	"copy": true, "write": true, "make": true,
}

// episodicMarkers indicate a recollection of an event: episodic memory.
var episodicMarkers = []string{
	" i ", " we ", "yesterday", "last week", "last month", "during the session",
	"we met", "we discussed", "today ", "earlier ", "remember when",
}

// entityRe matches 1–3 word Title Case sequences.
var entityRe = regexp.MustCompile(`\b([A-Z][a-zA-Z0-9-]+(?:\s+[A-Z][a-zA-Z0-9-]+){0,2})\b`)

// sentenceStarters are Title Case words that open sentences, not entities.
var sentenceStarters = map[string]bool{
	"The": true, "This": true, "That": true, "These": true, "Those": true,
	"When": true, "Then": true, "And": true, "But": true, "For": true,
	"With": true, "If": true, "In": true, "On": true, "At": true, "It": true,
	"I": true, "We": true, "A": true, "An": true, "Go": true, "Now": true,
}

// maxEntities caps extraction so a dense document can't blow up ingestion.
const maxEntities = 16

// Classify infers a MemoryType from content shape. Imperative/how-to text is
// procedural; first-person or timestamped narrative is episodic; everything
// else defaults to semantic.
func Classify(content string) domain.MemoryType {
	lower := " " + strings.ToLower(content) + " "
	first := strings.ToLower(strings.TrimSpace(content))
	if len(first) > 0 {
		firstWord := strings.SplitN(first, " ", 2)[0]
		if imperativeVerbs[firstWord] || strings.Contains(lower, " step 1") {
			return domain.MemoryTypeProcedural
		}
	}
	for _, m := range episodicMarkers {
		if strings.Contains(lower, m) {
			return domain.MemoryTypeEpisodic
		}
	}
	return domain.MemoryTypeSemantic
}

// ExtractEntities pulls Title Case sequences as candidate entities. Sequences
// at a sentence start are skipped (they are usually subjects, not names).
// IDs are deterministic (SHA-1 of the name) so re-encoding the same content
// resolves to the same entities.
func ExtractEntities(content string) []domain.Entity {
	seen := map[string]bool{}
	var out []domain.Entity
	for _, loc := range entityRe.FindAllStringIndex(content, -1) {
		if len(out) >= maxEntities {
			break
		}
		name := strings.TrimSpace(content[loc[0]:loc[1]])
		if sentenceStarters[name] || seen[name] {
			continue
		}
		// Skip a sequence that opens a sentence: preceded by '.', '!', '?' or start.
		if loc[0] == 0 || strings.ContainsRune(".!?", rune(content[loc[0]-1])) {
			continue
		}
		seen[name] = true
		out = append(out, domain.Entity{
			EntityID:   uuid.NewSHA1(uuid.NameSpaceOID, []byte("mneme/entity/"+name)),
			Name:       name,
			EntityType: domain.EntityConcept,
			CreatedAt:  timeNowUTC(),
		})
	}
	return out
}

// EstimateTokens approximates token count at ~4 chars/token, the usual
// byte-pair-encoding rule of thumb for English prose.
func EstimateTokens(content string) int {
	return (utf8.RuneCountInString(content) + 3) / 4
}
