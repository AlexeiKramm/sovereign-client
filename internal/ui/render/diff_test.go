package render

import (
	"reflect"
	"testing"
)

func TestTokenizeWords(t *testing.T) {
	tokens := TokenizeWords("Hello, world! It's a test... Right?")
	expected := []string{"hello", "world", "it's", "a", "test", "right"}
	if !reflect.DeepEqual(tokens, expected) {
		t.Fatalf("TokenizeWords got %v, want %v", tokens, expected)
	}
}

func TestCalculateVerbatim(t *testing.T) {
	res := Verbatim("Just speaking words here")
	if res.RawWords != 4 || res.DeliveredWords != 4 || res.WordsRemoved != 0 || res.WordsChanged != 0 || res.WordsAdded != 0 || res.WordsEdited() != 0 {
		t.Fatalf("unexpected verbatim result: %+v", res)
	}
}

func TestCalculateFillerRemoval(t *testing.T) {
	raw := "So um uh I went to the store"
	clean := "I went to the store."
	res := Calculate(raw, clean)

	if res.RawWords != 8 {
		t.Errorf("RawWords = %d, want 8", res.RawWords)
	}
	if res.DeliveredWords != 5 {
		t.Errorf("DeliveredWords = %d, want 5", res.DeliveredWords)
	}
	if res.WordsRemoved != 3 {
		t.Errorf("WordsRemoved = %d, want 3", res.WordsRemoved)
	}
	if res.WordsChanged != 0 {
		t.Errorf("WordsChanged = %d, want 0", res.WordsChanged)
	}
	if res.WordsAdded != 0 {
		t.Errorf("WordsAdded = %d, want 0", res.WordsAdded)
	}
	if res.WordsEdited() != 3 {
		t.Errorf("WordsEdited = %d, want 3", res.WordsEdited())
	}
}

func TestCalculateGrammarSubstitution(t *testing.T) {
	raw := "She dont know nothing"
	clean := "She doesn't know anything."
	res := Calculate(raw, clean)

	if res.RawWords != 4 || res.DeliveredWords != 4 {
		t.Errorf("word counts mismatch: %+v", res)
	}
	if res.WordsRemoved != 0 {
		t.Errorf("WordsRemoved = %d, want 0", res.WordsRemoved)
	}
	if res.WordsChanged != 2 {
		t.Errorf("WordsChanged = %d, want 2", res.WordsChanged)
	}
	if res.WordsEdited() != 2 {
		t.Errorf("WordsEdited = %d, want 2", res.WordsEdited())
	}
}

func TestCalculateMixedEdits(t *testing.T) {
	raw := "Well um she dont like it"
	clean := "She doesn't like it."
	res := Calculate(raw, clean)

	if res.RawWords != 6 || res.DeliveredWords != 4 {
		t.Errorf("word counts mismatch: %+v", res)
	}
	if res.WordsRemoved != 2 {
		t.Errorf("WordsRemoved = %d, want 2", res.WordsRemoved)
	}
	if res.WordsChanged != 1 {
		t.Errorf("WordsChanged = %d, want 1", res.WordsChanged)
	}
	if res.WordsEdited() != 3 {
		t.Errorf("WordsEdited = %d, want 3", res.WordsEdited())
	}
}

func TestCalculateInsertions(t *testing.T) {
	raw := "In spite of"
	clean := "In spite of it all."
	res := Calculate(raw, clean)

	if res.RawWords != 3 || res.DeliveredWords != 5 {
		t.Errorf("word counts mismatch: %+v", res)
	}
	if res.WordsRemoved != 0 {
		t.Errorf("WordsRemoved = %d, want 0", res.WordsRemoved)
	}
	if res.WordsChanged != 0 {
		t.Errorf("WordsChanged = %d, want 0", res.WordsChanged)
	}
	if res.WordsAdded != 2 {
		t.Errorf("WordsAdded = %d, want 2", res.WordsAdded)
	}
	if res.WordsEdited() != 2 {
		t.Errorf("WordsEdited = %d, want 2", res.WordsEdited())
	}
}

func TestCalculateEmpty(t *testing.T) {
	res := Calculate("", "")
	if res.RawWords != 0 || res.DeliveredWords != 0 || res.WordsEdited() != 0 {
		t.Errorf("expected all 0, got %+v", res)
	}
}

func TestCalculatePunctuationOnly(t *testing.T) {
	raw := "hello world how are you"
	clean := "Hello, world! How are you?"
	res := Calculate(raw, clean)

	if res.RawWords != 5 || res.DeliveredWords != 5 || res.WordsEdited() != 0 {
		t.Errorf("expected 0 edits for pure punctuation/casing change, got %+v", res)
	}
}

func TestComputeInlineDiffIdentical(t *testing.T) {
	text := "This is a clean sentence."
	diff := ComputeInlineDiff(text, text)
	if diff.HasEdits {
		t.Error("expected hasEdits = false")
	}
	if len(diff.Tokens) != 1 || diff.Tokens[0].Kind != TokenMatch || diff.Tokens[0].Text != text {
		t.Errorf("unexpected tokens: %+v", diff.Tokens)
	}
}

func TestComputeInlineDiffReCased(t *testing.T) {
	diff := ComputeInlineDiff("Make it work", "make it work")
	if !diff.HasEdits {
		t.Error("expected hasEdits = true")
	}
	var subs []DiffToken
	for _, tok := range diff.Tokens {
		if tok.Kind == TokenSubstitution {
			subs = append(subs, tok)
		}
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 substitution, got %d", len(subs))
	}
	if subs[0].Original != "Make" || subs[0].Replacement != "make" {
		t.Errorf("unexpected substitution: %+v", subs[0])
	}
}

func TestComputeInlineDiffPunctuationAdded(t *testing.T) {
	diff := ComputeInlineDiff("issue", "issue.")
	if !diff.HasEdits {
		t.Error("expected hasEdits = true")
	}
	if len(diff.Tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(diff.Tokens))
	}
	if diff.Tokens[0].Kind != TokenMatch || diff.Tokens[0].Text != "issue" {
		t.Errorf("token 0 mismatch: %+v", diff.Tokens[0])
	}
	if diff.Tokens[1].Kind != TokenInsertion || diff.Tokens[1].Text != "." {
		t.Errorf("token 1 mismatch: %+v", diff.Tokens[1])
	}
}

func TestComputeInlineDiffPunctuationRemoved(t *testing.T) {
	diff := ComputeInlineDiff("again, so", "again so")
	if !diff.HasEdits {
		t.Error("expected hasEdits = true")
	}
	if len(diff.Tokens) != 3 {
		t.Fatalf("expected 3 tokens, got %d", len(diff.Tokens))
	}
	if diff.Tokens[0].Kind != TokenMatch || diff.Tokens[0].Text != "again" {
		t.Errorf("token 0 mismatch: %+v", diff.Tokens[0])
	}
	if diff.Tokens[1].Kind != TokenRemoval || diff.Tokens[1].Text != "," {
		t.Errorf("token 1 mismatch: %+v", diff.Tokens[1])
	}
	if diff.Tokens[2].Kind != TokenMatch || diff.Tokens[2].Text != "so" {
		t.Errorf("token 2 mismatch: %+v", diff.Tokens[2])
	}
}

func TestComputeInlineDiffDeviceRecord(t *testing.T) {
	raw := "Three restarting sovereign on the client seems to Make it work again, so this may be the client side issue"
	clean := "Three restarting sovereign on the client seems to make it work again, so this may be the client side issue."
	diff := ComputeInlineDiff(raw, clean)

	if !diff.HasEdits {
		t.Error("expected hasEdits = true")
	}
	if diff.Tokens[8].Kind != TokenSubstitution || diff.Tokens[8].Original != "Make" || diff.Tokens[8].Replacement != "make" {
		t.Errorf("token 8 mismatch: %+v", diff.Tokens[8])
	}
	last := diff.Tokens[len(diff.Tokens)-1]
	penult := diff.Tokens[len(diff.Tokens)-2]
	if last.Kind != TokenInsertion || last.Text != "." {
		t.Errorf("last token mismatch: %+v", last)
	}
	if penult.Kind != TokenMatch || penult.Text != "issue" {
		t.Errorf("penultimate token mismatch: %+v", penult)
	}
}

func TestComputeInlineDiffFillerRemoval(t *testing.T) {
	raw := "So um uh I went to the store"
	clean := "So, I went to the store."
	diff := ComputeInlineDiff(raw, clean)

	if !diff.HasEdits {
		t.Error("expected hasEdits = true")
	}
	var removals []DiffToken
	for _, tok := range diff.Tokens {
		if tok.Kind == TokenRemoval {
			removals = append(removals, tok)
		}
	}
	if len(removals) != 1 {
		t.Fatalf("expected 1 removal, got %d", len(removals))
	}
	if removals[0].Text != "um uh" {
		t.Errorf("expected 'um uh', got %q", removals[0].Text)
	}
}

func TestComputeInlineDiffGrammarSubstitution(t *testing.T) {
	raw := "She dont know"
	clean := "She doesn't know."
	diff := ComputeInlineDiff(raw, clean)

	if !diff.HasEdits {
		t.Error("expected hasEdits = true")
	}
	var subs []DiffToken
	for _, tok := range diff.Tokens {
		if tok.Kind == TokenSubstitution {
			subs = append(subs, tok)
		}
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 substitution, got %d", len(subs))
	}
	if subs[0].Original != "dont" || subs[0].Replacement != "doesn't" {
		t.Errorf("substitution mismatch: %+v", subs[0])
	}
}

func TestComputeInlineDiffMultiWordSubstitution(t *testing.T) {
	raw := "I wanna go"
	clean := "I want to go."
	diff := ComputeInlineDiff(raw, clean)

	if !diff.HasEdits {
		t.Error("expected hasEdits = true")
	}
	var subs []DiffToken
	for _, tok := range diff.Tokens {
		if tok.Kind == TokenSubstitution {
			subs = append(subs, tok)
		}
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 substitution, got %d", len(subs))
	}
	if subs[0].Original != "wanna" || subs[0].Replacement != "want to" {
		t.Errorf("substitution mismatch: %+v", subs[0])
	}
}

func TestComputeInlineDiffInsertion(t *testing.T) {
	raw := "went store"
	clean := "went to the store."
	diff := ComputeInlineDiff(raw, clean)

	if !diff.HasEdits {
		t.Error("expected hasEdits = true")
	}
	var inss []DiffToken
	for _, tok := range diff.Tokens {
		if tok.Kind == TokenInsertion {
			inss = append(inss, tok)
		}
	}
	if len(inss) != 2 {
		t.Fatalf("expected 2 insertions, got %d", len(inss))
	}
	if inss[0].Text != "to the" {
		t.Errorf("insertion 0 = %q, want 'to the'", inss[0].Text)
	}
	if inss[1].Text != "." {
		t.Errorf("insertion 1 = %q, want '.'", inss[1].Text)
	}
}

func TestNeedsSpaceBefore(t *testing.T) {
	if NeedsSpaceBefore(DiffToken{Kind: TokenInsertion, Text: "."}) {
		t.Error("period insertion should not need leading space")
	}
	if NeedsSpaceBefore(DiffToken{Kind: TokenInsertion, Text: ","}) {
		t.Error("comma insertion should not need leading space")
	}
	if NeedsSpaceBefore(DiffToken{Kind: TokenRemoval, Text: "."}) {
		t.Error("period removal should not need leading space")
	}
	if !NeedsSpaceBefore(DiffToken{Kind: TokenMatch, Text: "issue"}) {
		t.Error("match token should need leading space")
	}
	if !NeedsSpaceBefore(DiffToken{Kind: TokenInsertion, Text: "to the"}) {
		t.Error("word insertion should need leading space")
	}
	if !NeedsSpaceBefore(DiffToken{Kind: TokenRemoval, Text: "um uh"}) {
		t.Error("word removal should need leading space")
	}
	if !NeedsSpaceBefore(DiffToken{Kind: TokenSubstitution, Original: "Make", Replacement: "make"}) {
		t.Error("substitution token should need leading space")
	}
}
