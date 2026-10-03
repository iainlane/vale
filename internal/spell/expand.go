package spell

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var defaultConfig = sync.OnceValues(func() (*dictConfig, error) {
	return newDictConfig(bytes.NewReader(defaultAff))
})

// affixFiles holds each `.aff` file ExpandWith has read, by path.
var affixFiles sync.Map // path -> *dictConfig

// Expand returns the words that word with the given affix flags, such as
// `failover` and `S`, generates under Vale's default affix file.
func Expand(word, flags string) ([]string, error) {
	aff, err := defaultConfig()
	if err != nil {
		return nil, err
	}
	return expandWith(aff, "the default dictionary", word, flags)
}

// ExpandWith is Expand under the `.aff` file at path.
func ExpandWith(path, word, flags string) ([]string, error) {
	cached, ok := affixFiles.Load(path)
	if !ok {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		raw, _, err = decodeDictionary(raw, nil)
		if err != nil {
			return nil, err
		}
		aff, err := newDictConfig(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		cached, _ = affixFiles.LoadOrStore(path, aff)
	}
	return expandWith(cached.(*dictConfig), filepath.Base(path), word, flags) //nolint:errcheck // only *dictConfig is stored
}

func expandWith(aff *dictConfig, name, word, flags string) ([]string, error) {
	if flags == "" {
		return []string{word}, nil
	} else if word == "" {
		return nil, fmt.Errorf("flags '%s' need a word", flags)
	}

	keys := aff.parseFlags(flags)
	for _, flag := range keys {
		if _, ok := aff.AffixMap[flag]; !ok {
			return nil, fmt.Errorf("'%s' isn't an affix flag in %s", flag, name)
		}
	}

	forms := aff.expander(nil, nil).root(word, keys)
	seen := make(map[string]bool, len(forms))
	words := make([]string, 0, len(forms))
	for _, f := range forms {
		if !seen[f.Word] {
			seen[f.Word] = true
			words = append(words, f.Word)
		}
	}
	return words, nil
}
