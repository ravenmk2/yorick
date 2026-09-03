package core

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const regexPrefix = "re:"

type Pattern struct {
	Raw string
	re  *regexp.Regexp
}

func CompilePattern(raw string) (*Pattern, error) {
	if strings.HasPrefix(raw, regexPrefix) {
		re, err := regexp.Compile(strings.TrimPrefix(raw, regexPrefix))
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern %q: %w", raw, err)
		}
		return &Pattern{Raw: raw, re: re}, nil
	}
	if !doublestar.ValidatePattern(raw) {
		return nil, fmt.Errorf("invalid glob pattern %q", raw)
	}
	return &Pattern{Raw: raw}, nil
}

func (p *Pattern) Matches(candidate string) bool {
	candidate = filepath.ToSlash(candidate)
	if p.re != nil {
		return p.re.MatchString(candidate)
	}
	matched, err := doublestar.Match(p.Raw, candidate)
	return err == nil && matched
}

type PatternList []*Pattern

func CompilePatternList(raws []string) (PatternList, error) {
	patterns := make(PatternList, 0, len(raws))
	for _, raw := range raws {
		pattern, err := CompilePattern(raw)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

func (pl PatternList) MatchesAny(candidate string) bool {
	for _, pattern := range pl {
		if pattern.Matches(candidate) {
			return true
		}
	}
	return false
}

// FilterCandidates keeps a candidate iff it matches at least one include
// pattern (or include is empty) and matches no exclude pattern.
func FilterCandidates(candidates []string, include, exclude PatternList) []string {
	kept := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if len(include) > 0 && !include.MatchesAny(candidate) {
			continue
		}
		if exclude.MatchesAny(candidate) {
			continue
		}
		kept = append(kept, candidate)
	}
	return kept
}
