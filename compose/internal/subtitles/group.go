package subtitles

import "strings"

// Line is one subtitle line: the words it covers, joined for display, and
// the line's own [Start, End] span (its first word's Start, its last
// word's End).
type Line struct {
	Text  string
	Start float64
	End   float64
}

// endsSentence reports whether word (as it literally appears in the
// source text, punctuation included) ends a sentence — ., !, ?, or … ,
// optionally followed by a closing quote or bracket.
func endsSentence(word string) bool {
	w := strings.TrimRight(word, "\"')]")
	if w == "" {
		return false
	}
	switch w[len(w)-1] {
	case '.', '!', '?':
		return true
	}
	return strings.HasSuffix(w, "…")
}

// GroupLines turns words into caption cards. Each sentence is a hard
// boundary (a card never spans a full stop), and a sentence longer than
// max words is split into cards of BALANCED size rather than greedily
// filled to max — so an 8-word sentence with max 7 becomes 4+4, never 7+1
// (an orphaned last word on its own card, which reads badly and wastes the
// space a fuller card would use). min is retained for API stability but no
// longer forces anything: balanced splitting already avoids tiny tails,
// and a genuinely short standalone sentence is a small card on purpose.
func GroupLines(words []Word, min, max int) []Line {
	if max <= 0 {
		max = 9
	}

	var lines []Line
	for _, sentence := range splitIntoSentences(words) {
		for _, card := range balancedSplit(sentence, max) {
			lines = append(lines, lineFromWords(card))
		}
	}
	return lines
}

// splitIntoSentences groups consecutive words into sentences, cutting
// after each word that ends one. Trailing words with no terminal
// punctuation form a final sentence (real text doesn't guarantee one).
func splitIntoSentences(words []Word) [][]Word {
	var sentences [][]Word
	var cur []Word
	for _, w := range words {
		cur = append(cur, w)
		if endsSentence(w.Text) {
			sentences = append(sentences, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		sentences = append(sentences, cur)
	}
	return sentences
}

// balancedSplit divides one sentence into ceil(len/max) cards as evenly
// sized as possible — every card is at most max words, and their sizes
// differ by at most one, so there's never a lone-word remainder.
func balancedSplit(sentence []Word, max int) [][]Word {
	n := len(sentence)
	if n == 0 {
		return nil
	}
	numCards := (n + max - 1) / max // ceil(n/max)
	base := n / numCards
	rem := n % numCards

	var cards [][]Word
	i := 0
	for c := 0; c < numCards; c++ {
		size := base
		if c < rem {
			size++ // spread the remainder across the first cards
		}
		cards = append(cards, sentence[i:i+size])
		i += size
	}
	return cards
}

func lineFromWords(ws []Word) Line {
	texts := make([]string, len(ws))
	for i, w := range ws {
		texts[i] = w.Text
	}
	return Line{Text: strings.Join(texts, " "), Start: ws[0].Start, End: ws[len(ws)-1].End}
}
