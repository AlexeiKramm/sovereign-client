package render

import (
	"math"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/image/font"
)

// WordDiffResult stores the count of word-level edits between raw ASR and post-cleanup text.
type WordDiffResult struct {
	RawWords       int
	DeliveredWords int
	WordsRemoved   int
	WordsChanged   int
	WordsAdded     int
}

// WordsEdited returns the total word-level edits: removed + changed + added (D-124).
func (r WordDiffResult) WordsEdited() int {
	return r.WordsRemoved + r.WordsChanged + r.WordsAdded
}

var tokenWordRegex = regexp.MustCompile(`[\p{L}\p{N}']+`)

// TokenizeWords splits text into normalized lowercase word tokens, stripping surrounding quotes/punctuation.
// E.g. "Hello, world! It's a test... Right?" -> ["hello", "world", "it's", "a", "test", "right"].
func TokenizeWords(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	matches := tokenWordRegex.FindAllString(strings.ToLower(text), -1)
	var out []string
	for _, m := range matches {
		trimmed := strings.Trim(m, "'")
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// Verbatim returns a diff result when no cleanup was performed.
func Verbatim(rawText string) WordDiffResult {
	words := len(TokenizeWords(rawText))
	return WordDiffResult{
		RawWords:       words,
		DeliveredWords: words,
	}
}

// Remote maps server correction count onto WordsChanged, matching Android WordDiffCalculator.remote.
func Remote(deliveredText string, corrections int) WordDiffResult {
	words := len(TokenizeWords(deliveredText))
	if corrections < 0 {
		corrections = 0
	}
	return WordDiffResult{
		RawWords:       words,
		DeliveredWords: words,
		WordsChanged:   corrections,
	}
}

// Calculate compares raw ASR text and cleaned text using dynamic programming sequence alignment
// to identify removed words (fillers), changed words (grammar/substitutions), and added words.
func Calculate(rawText, cleanedText string) WordDiffResult {
	rawTokens := TokenizeWords(rawText)
	cleanTokens := TokenizeWords(cleanedText)
	m, n := len(rawTokens), len(cleanTokens)

	if m == 0 && n == 0 {
		return WordDiffResult{}
	}
	if m == 0 {
		return WordDiffResult{DeliveredWords: n, WordsAdded: n}
	}
	if n == 0 {
		return WordDiffResult{RawWords: m, WordsRemoved: m}
	}

	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
		dp[i][0] = i
	}
	for j := 0; j <= n; j++ {
		dp[0][j] = j
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			cost := 1
			if rawTokens[i-1] == cleanTokens[j-1] {
				cost = 0
			}
			del := dp[i-1][j] + 1
			ins := dp[i][j-1] + 1
			sub := dp[i-1][j-1] + cost
			minVal := del
			if ins < minVal {
				minVal = ins
			}
			if sub < minVal {
				minVal = sub
			}
			dp[i][j] = minVal
		}
	}

	i, j := m, n
	var removed, changed, added int

	for i > 0 || j > 0 {
		if i > 0 && j > 0 {
			match := rawTokens[i-1] == cleanTokens[j-1]
			cost := 1
			if match {
				cost = 0
			}
			if dp[i][j] == dp[i-1][j-1]+cost {
				if !match {
					changed++
				}
				i--
				j--
				continue
			}
		}
		if i > 0 && dp[i][j] == dp[i-1][j]+1 {
			removed++
			i--
		} else if j > 0 && dp[i][j] == dp[i][j-1]+1 {
			added++
			j--
		} else {
			if i > 0 {
				removed++
				i--
			} else if j > 0 {
				added++
				j--
			}
		}
	}

	return WordDiffResult{
		RawWords:       m,
		DeliveredWords: n,
		WordsRemoved:   removed,
		WordsChanged:   changed,
		WordsAdded:     added,
	}
}

// DiffTokenKind defines the type of atomic diff element.
type DiffTokenKind int

const (
	TokenMatch DiffTokenKind = iota
	TokenRemoval
	TokenSubstitution
	TokenInsertion
)

// DiffToken is an atomic segment of an inline speech diff.
type DiffToken struct {
	Kind        DiffTokenKind
	Text        string // Text for Match, Removal, Insertion
	Original    string // Text for Substitution
	Replacement string // Text for Substitution
}

// InlineDiff represents the word-level differences between raw speech and post-cleanup text.
type InlineDiff struct {
	Tokens   []DiffToken
	HasEdits bool
}

type diffOpKind int

const (
	diffMatch diffOpKind = iota
	diffSub
	diffDel
	diffIns
)

type stepOp struct {
	kind      diffOpKind
	rawWord   string
	cleanWord string
}

type punctSplit struct {
	matchText string
	editText  string
	added     bool
}

func normalizeForDiff(token string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(token) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isPunctuationOnly(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func punctuationSplit(rawWord, cleanWord string) *punctSplit {
	if rawWord == "" || cleanWord == "" {
		return nil
	}
	if len(cleanWord) > len(rawWord) && strings.HasPrefix(cleanWord, rawWord) {
		tail := cleanWord[len(rawWord):]
		if isPunctuationOnly(tail) {
			return &punctSplit{matchText: rawWord, editText: tail, added: true}
		}
	}
	if len(rawWord) > len(cleanWord) && strings.HasPrefix(rawWord, cleanWord) {
		tail := rawWord[len(cleanWord):]
		if isPunctuationOnly(tail) {
			return &punctSplit{matchText: cleanWord, editText: tail, added: false}
		}
	}
	return nil
}

// NeedsSpaceBefore returns whether a diff token begins a new word and so needs a space before it.
// Punctuation the cleanup added or removed attaches directly to its word without leading space.
func NeedsSpaceBefore(token DiffToken) bool {
	switch token.Kind {
	case TokenRemoval, TokenInsertion:
		return !isPunctuationOnly(token.Text)
	case TokenMatch, TokenSubstitution:
		return true
	default:
		return true
	}
}

// ComputeInlineDiff computes an inline word-level diff aligning rawText against cleanedText,
// mirroring Android WordDiffCalculator.computeInlineDiff.
func ComputeInlineDiff(rawText, cleanedText string) InlineDiff {
	rawTrim := strings.TrimSpace(rawText)
	cleanTrim := strings.TrimSpace(cleanedText)

	if rawTrim == "" && cleanTrim == "" {
		return InlineDiff{HasEdits: false}
	}
	if rawTrim == "" {
		return InlineDiff{Tokens: []DiffToken{{Kind: TokenMatch, Text: cleanTrim}}, HasEdits: false}
	}
	if cleanTrim == "" {
		return InlineDiff{Tokens: []DiffToken{{Kind: TokenRemoval, Text: rawTrim}}, HasEdits: true}
	}
	if rawTrim == cleanTrim {
		return InlineDiff{Tokens: []DiffToken{{Kind: TokenMatch, Text: cleanTrim}}, HasEdits: false}
	}

	rawWords := strings.Fields(rawTrim)
	cleanWords := strings.Fields(cleanTrim)

	if len(rawWords) == 0 && len(cleanWords) == 0 {
		return InlineDiff{HasEdits: false}
	}
	if len(rawWords) == 0 {
		tokens := make([]DiffToken, len(cleanWords))
		for idx, w := range cleanWords {
			tokens[idx] = DiffToken{Kind: TokenInsertion, Text: w}
		}
		return InlineDiff{Tokens: tokens, HasEdits: true}
	}
	if len(cleanWords) == 0 {
		tokens := make([]DiffToken, len(rawWords))
		for idx, w := range rawWords {
			tokens[idx] = DiffToken{Kind: TokenRemoval, Text: w}
		}
		return InlineDiff{Tokens: tokens, HasEdits: true}
	}

	m := len(rawWords)
	n := len(cleanWords)

	dp := make([][]float64, m+1)
	for i := range dp {
		dp[i] = make([]float64, n+1)
		dp[i][0] = float64(i)
	}
	for j := 0; j <= n; j++ {
		dp[0][j] = float64(j)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			r := rawWords[i-1]
			c := cleanWords[j-1]
			normR := normalizeForDiff(r)
			normC := normalizeForDiff(c)

			var cost float64
			if r == c {
				cost = 0.0
			} else if normR == normC && normR != "" {
				cost = 0.05
			} else {
				cost = 1.0
			}

			del := dp[i-1][j] + 1.0
			ins := dp[i][j-1] + 1.0
			sub := dp[i-1][j-1] + cost
			minVal := del
			if ins < minVal {
				minVal = ins
			}
			if sub < minVal {
				minVal = sub
			}
			dp[i][j] = minVal
		}
	}

	i := m
	j := n
	var rawOps []stepOp

	for i > 0 || j > 0 {
		if i > 0 && j > 0 {
			r := rawWords[i-1]
			c := cleanWords[j-1]
			normR := normalizeForDiff(r)
			normC := normalizeForDiff(c)

			var cost float64
			if r == c {
				cost = 0.0
			} else if normR == normC && normR != "" {
				cost = 0.05
			} else {
				cost = 1.0
			}

			if math.Abs(dp[i][j]-(dp[i-1][j-1]+cost)) < 1e-6 {
				if r == c {
					rawOps = append(rawOps, stepOp{kind: diffMatch, rawWord: r, cleanWord: c})
				} else {
					rawOps = append(rawOps, stepOp{kind: diffSub, rawWord: r, cleanWord: c})
				}
				i--
				j--
				continue
			}
		}
		if i > 0 && (j == 0 || math.Abs(dp[i][j]-(dp[i-1][j]+1.0)) < 1e-6) {
			rawOps = append(rawOps, stepOp{kind: diffDel, rawWord: rawWords[i-1]})
			i--
		} else if j > 0 {
			rawOps = append(rawOps, stepOp{kind: diffIns, cleanWord: cleanWords[j-1]})
			j--
		}
	}

	// Reverse to forward order
	for l, r := 0, len(rawOps)-1; l < r; l, r = l+1, r-1 {
		rawOps[l], rawOps[r] = rawOps[r], rawOps[l]
	}

	var tokens []DiffToken
	var pendingRaw []string
	var pendingClean []string

	flushPending := func() {
		if len(pendingRaw) == 0 && len(pendingClean) == 0 {
			return
		}
		if len(pendingRaw) > 0 && len(pendingClean) == 0 {
			tokens = append(tokens, DiffToken{
				Kind: TokenRemoval,
				Text: strings.Join(pendingRaw, " "),
			})
		} else if len(pendingRaw) == 0 && len(pendingClean) > 0 {
			tokens = append(tokens, DiffToken{
				Kind: TokenInsertion,
				Text: strings.Join(pendingClean, " "),
			})
		} else {
			tokens = append(tokens, DiffToken{
				Kind:        TokenSubstitution,
				Original:    strings.Join(pendingRaw, " "),
				Replacement: strings.Join(pendingClean, " "),
			})
		}
		pendingRaw = nil
		pendingClean = nil
	}

	for _, op := range rawOps {
		switch op.kind {
		case diffMatch:
			flushPending()
			tokens = append(tokens, DiffToken{Kind: TokenMatch, Text: op.cleanWord})
		case diffDel:
			if op.rawWord != "" {
				pendingRaw = append(pendingRaw, op.rawWord)
			}
		case diffIns:
			if op.cleanWord != "" {
				pendingClean = append(pendingClean, op.cleanWord)
			}
		case diffSub:
			split := punctuationSplit(op.rawWord, op.cleanWord)
			if split != nil {
				flushPending()
				tokens = append(tokens, DiffToken{Kind: TokenMatch, Text: split.matchText})
				if split.added {
					tokens = append(tokens, DiffToken{Kind: TokenInsertion, Text: split.editText})
				} else {
					tokens = append(tokens, DiffToken{Kind: TokenRemoval, Text: split.editText})
				}
			} else {
				if op.rawWord != "" {
					pendingRaw = append(pendingRaw, op.rawWord)
				}
				if op.cleanWord != "" {
					pendingClean = append(pendingClean, op.cleanWord)
				}
			}
		}
	}
	flushPending()

	hasEdits := false
	for _, t := range tokens {
		if t.Kind != TokenMatch {
			hasEdits = true
			break
		}
	}
	return InlineDiff{Tokens: tokens, HasEdits: hasEdits}
}

// DiffWords formats an InlineDiff into styled render words with line-wrapping clusters.
func DiffWords(face font.Face, diff InlineDiff, theme Theme) []word {
	var words []word
	bracketCol := theme.OnSurfaceVariant

	for i, token := range diff.Tokens {
		// If this token does not need a space before it (e.g. attached punctuation), glue to previous word.
		if i > 0 && !NeedsSpaceBefore(token) && len(words) > 0 {
			words[len(words)-1].glueToNext = true
		}

		switch token.Kind {
		case TokenMatch:
			matches := strings.Fields(token.Text)
			for _, m := range matches {
				w := float64(font.MeasureString(face, m).Ceil())
				words = append(words, word{
					text:       m,
					col:        theme.OnSurface,
					width:      w,
					glueToNext: false,
				})
			}

		case TokenRemoval:
			removals := strings.Fields(token.Text)
			wBracketL := float64(font.MeasureString(face, "[").Ceil())
			wBracketR := float64(font.MeasureString(face, "]").Ceil())

			words = append(words, word{
				text:       "[",
				col:        bracketCol,
				width:      wBracketL,
				glueToNext: true,
			})
			for j, r := range removals {
				glue := (j == len(removals)-1) // Glue last word to "]"
				wR := float64(font.MeasureString(face, r).Ceil())
				words = append(words, word{
					text:       r,
					col:        theme.Error,
					width:      wR,
					strike:     true,
					glueToNext: glue,
				})
			}
			words = append(words, word{
				text:       "]",
				col:        bracketCol,
				width:      wBracketR,
				glueToNext: false,
			})

		case TokenSubstitution:
			origs := strings.Fields(token.Original)
			repls := strings.Fields(token.Replacement)
			wBracketL := float64(font.MeasureString(face, "[").Ceil())
			wBracketR := float64(font.MeasureString(face, "]").Ceil())
			wArrow := float64(font.MeasureString(face, "→").Ceil())

			words = append(words, word{
				text:       "[",
				col:        bracketCol,
				width:      wBracketL,
				glueToNext: true,
			})
			for _, o := range origs {
				wO := float64(font.MeasureString(face, o).Ceil())
				words = append(words, word{
					text:       o,
					col:        theme.Error,
					width:      wO,
					strike:     true,
					glueToNext: false,
				})
			}
			words = append(words, word{
				text:       "→",
				col:        bracketCol,
				width:      wArrow,
				glueToNext: false,
			})
			for j, repl := range repls {
				glue := (j == len(repls)-1) // Glue last word to "]"
				wRepl := float64(font.MeasureString(face, repl).Ceil())
				words = append(words, word{
					text:       repl,
					col:        theme.Success,
					width:      wRepl,
					glueToNext: glue,
				})
			}
			words = append(words, word{
				text:       "]",
				col:        bracketCol,
				width:      wBracketR,
				glueToNext: false,
			})

		case TokenInsertion:
			inss := strings.Fields(token.Text)
			wBracketL := float64(font.MeasureString(face, "[+ ").Ceil())
			wBracketR := float64(font.MeasureString(face, "]").Ceil())

			words = append(words, word{
				text:       "[+ ",
				col:        bracketCol,
				width:      wBracketL,
				glueToNext: true,
			})
			for j, ins := range inss {
				glue := (j == len(inss)-1) // Glue last word to "]"
				wIns := float64(font.MeasureString(face, ins).Ceil())
				words = append(words, word{
					text:       ins,
					col:        theme.Success,
					width:      wIns,
					glueToNext: glue,
				})
			}
			words = append(words, word{
				text:       "]",
				col:        bracketCol,
				width:      wBracketR,
				glueToNext: false,
			})
		}
	}

	return words
}
