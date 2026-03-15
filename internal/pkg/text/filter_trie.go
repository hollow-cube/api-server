package text

import "strings"

type negative struct {
	Prefix string
	Suffix string
}

func (n negative) matches(text string, start int, end int) bool {
	prefixIndex := start - len(n.Prefix)
	suffixIndex := end + len(n.Suffix)

	if prefixIndex < 0 || suffixIndex > len(text) {
		return false
	}

	return text[prefixIndex:start] == n.Prefix && text[end:suffixIndex] == n.Suffix
}

type node struct {
	Children  map[rune]*node
	Negatives []negative
	IsEnd     bool
}

func newNode() *node {
	return &node{Children: make(map[rune]*node), Negatives: []negative{}, IsEnd: false}
}

type FilterTrie struct {
	root *node
}

func (f *FilterTrie) Put(word string, negatives ...string) {
	word = strings.ToLower(word)

	if f.root == nil {
		f.root = newNode()
	}

	current := f.root
	for _, char := range word {
		if current.Children[char] == nil {
			current.Children[char] = newNode()
		}
		current = current.Children[char]
	}
	current.IsEnd = true

	for _, neg := range negatives {
		neg = strings.ToLower(neg)
		prefix := neg[:strings.Index(neg, word)]
		suffix := neg[strings.Index(neg, word)+len(word):]
		current.Negatives = append(current.Negatives, negative{Prefix: prefix, Suffix: suffix})
	}
}

func (f *FilterTrie) Test(text string) *string {
	for i := 0; i < len(text); i++ {
		if matched := f.testAt(text, i); matched != nil {
			return matched
		}
	}
	return nil
}

func (f *FilterTrie) testAt(text string, index int) *string {
	node := f.root
	for i := index; i < len(text); i++ {
		char := rune(text[i])
		node = node.Children[char]
		if node == nil {
			return nil
		} else if node.IsEnd {
			for _, neg := range node.Negatives {
				if neg.matches(text, index, i+1) {
					if len(node.Children) == 0 {
						return nil
					}

					continue
				}
			}

			matched := text[index : i+1]
			return &matched
		}
	}
	return nil
}
