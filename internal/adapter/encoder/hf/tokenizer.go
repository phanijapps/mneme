package hf

import (
	"strings"
	"unicode"
)

// Special tokens for BERT-family sentence-transformers models.
const (
	clsToken = "[CLS]"
	sepToken = "[SEP]"
	unkToken = "[UNK]"
	padToken = "[PAD]"
)

// tokenRow is one encoded text: the real (unpadded) token ids.
type tokenRow struct {
	ids []int64
}

// wordPiece is a minimal BERT WordPiece tokenizer. It covers the common
// sentence-transformers case: basic normalization (whitespace cleanup,
// accent stripping is intentionally NOT applied — vocab differences make
// it model-specific), lowercasing, punctuation splitting, longest-match
// subword decomposition with ## prefixes, and special-token wrapping.
type wordPiece struct {
	vocab     map[string]int
	clsID     int
	sepID     int
	unkID     int
	maxChars  int
}

// newWordPiece resolves special tokens against vocab, falling back to
// [UNK] ids when the vocab lacks them.
func newWordPiece(vocab map[string]int) *wordPiece {
	unk := vocab[unkToken]
	return &wordPiece{
		vocab:    vocab,
		clsID:    vocabOr(vocab, clsToken, unk),
		sepID:    vocabOr(vocab, sepToken, unk),
		unkID:    unk,
		maxChars: 10000,
	}
}

func vocabOr(vocab map[string]int, token string, fallback int) int {
	if id, ok := vocab[token]; ok {
		return id
	}
	return fallback
}

// encodeBatch encodes every text, left-truncating to maxLen and padding to
// the longest row via the caller (ids are unpadded here). Returns rows and
// the batch sequence length (max row length).
func (w *wordPiece) encodeBatch(texts []string, maxLen int) (rows []tokenRow, seqLen int) {
	if maxLen <= 0 {
		maxLen = DefaultMaxLen
	}
	rows = make([]tokenRow, len(texts))
	seqLen = 0
	for i, text := range texts {
		ids := w.Encode(text, maxLen)
		rows[i] = tokenRow{ids: ids}
		if len(ids) > seqLen {
			seqLen = len(ids)
		}
	}
	if seqLen == 0 {
		seqLen = 1 // degenerate batch: one PAD position
	}
	return rows, seqLen
}

// Encode turns text into special-token-wrapped WordPiece ids, capped at
// maxLen total positions.
func (w *wordPiece) Encode(text string, maxLen int) []int64 {
	if maxLen <= 0 {
		maxLen = DefaultMaxLen
	}
	if len(text) > w.maxChars {
		text = text[:w.maxChars]
	}
	words := basicTokenize(text)
	ids := make([]int64, 0, len(words)+2)
	ids = append(ids, int64(w.clsID))
	for _, word := range words {
		subs := w.splitSubwords(word)
		if subs == nil {
			// Whole word unmatched: one [UNK], like BERT.
			ids = append(ids, int64(w.unkID))
			if len(ids) >= maxLen-1 {
				break
			}
			continue
		}
		for _, sub := range subs {
			id, ok := w.vocab[sub]
			if !ok {
				id = w.unkID
			}
			ids = append(ids, int64(id))
			if len(ids) >= maxLen-1 {
				// Reserve the final position for [SEP].
				break
			}
		}
		if len(ids) >= maxLen-1 {
			break
		}
	}
	ids = append(ids, int64(w.sepID))
	if len(ids) > maxLen {
		ids = ids[:maxLen]
	}
	return ids
}

// basicTokenize applies BERT basic normalization: lowercase, split on
// whitespace, isolate punctuation as single-character words.
func basicTokenize(text string) []string {
	text = strings.ToLower(text)
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			flush()
		case isPunct(r):
			flush()
			words = append(words, string(r))
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return words
}

// isPunct matches the ASCII punctuation set BERT isolates.
func isPunct(r rune) bool {
	switch r {
	case '!', '"', '#', '$', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.',
		'/', ':', ';', '<', '=', '>', '?', '@', '[', '\\', ']', '^', '_', '`',
		'{', '|', '}', '~':
		return true
	}
	return false
}

// splitSubwords decomposes one word into WordPiece segments via
// longest-match-first, prefixing continuations with ##.
func (w *wordPiece) splitSubwords(word string) []string {
	var out []string
	rest := word
	first := true
	for len(rest) > 0 {
		match, n := w.longestMatch(rest)
		if n == 0 {
			// No vocab entry at all: whole word becomes [UNK].
			return nil
		}
		if first {
			out = append(out, match)
			first = false
		} else {
			out = append(out, "##"+match)
		}
		rest = rest[n:]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// longestMatch finds the longest vocab prefix of s (trying both the bare
// and ##-prefixed forms). Returns the matched token and its byte length;
// ("", 0) means no match.
func (w *wordPiece) longestMatch(s string) (string, int) {
	// Greedy longest-first; vocab tokens are short so the scan is cheap.
	for end := len(s); end > 0; end-- {
		cand := s[:end]
		if _, ok := w.vocab[cand]; ok {
			return cand, end
		}
		if _, ok := w.vocab["##"+cand]; ok {
			return cand, end
		}
	}
	return "", 0
}
