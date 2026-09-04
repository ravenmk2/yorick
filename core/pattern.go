package core

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

const regexPrefix = "re:"

// Rule is one include/exclude entry. Type selects what the rule applies to:
// "dir", "file" or "any" (empty means "any"). Depth is a selection-level
// limit (include rules only): a candidate matches only when its level — 1
// for a direct child of the enumerated root — does not exceed Depth.
type Rule struct {
	Type    string
	Pattern string
	Depth   int

	re *regexp.Regexp
}

// UnmarshalYAML accepts a scalar (shorthand for {type: any}) or a mapping
// with optional type/depth and required pattern. The shorthand always gets
// the default depth.
func (r *Rule) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if err := value.Decode(&r.Pattern); err != nil {
			return err
		}
		r.Type = "any"
		r.Depth = 1
		return nil
	case yaml.MappingNode:
		var raw struct {
			Type    string `yaml:"type"`
			Pattern string `yaml:"pattern"`
			Depth   any    `yaml:"depth"`
		}
		if err := value.Decode(&raw); err != nil {
			return err
		}
		r.Type = raw.Type
		r.Pattern = raw.Pattern
		r.Depth = 1
		if r.Type == "" {
			r.Type = "any"
		}
		if r.Type != "dir" && r.Type != "file" && r.Type != "any" {
			return fmt.Errorf("invalid rule type %q (expected dir, file or any)", r.Type)
		}
		if r.Pattern == "" {
			return fmt.Errorf("rule pattern is required")
		}
		switch d := raw.Depth.(type) {
		case nil:
		case int:
			if d < 1 {
				return fmt.Errorf("invalid rule depth %d (expected >= 1)", d)
			}
			r.Depth = d
		case string:
			// A deferred expression is re-decoded after interpolation at
			// runtime; only its post-evaluation form is validated there.
			if !strings.Contains(d, exprOpen) {
				return fmt.Errorf("invalid rule depth %q (expected an integer >= 1)", d)
			}
		default:
			return fmt.Errorf("invalid rule depth %v (expected an integer >= 1)", d)
		}
		return nil
	default:
		return fmt.Errorf("invalid rule: expected a pattern string or a {type, pattern} mapping")
	}
}

// compile validates the rule and pre-compiles a regex pattern; glob patterns
// are only validated (doublestar compiles per Match call).
func (r *Rule) compile() error {
	if r.Type == "" {
		r.Type = "any"
	}
	if r.Type != "dir" && r.Type != "file" && r.Type != "any" {
		return fmt.Errorf("invalid rule type %q (expected dir, file or any)", r.Type)
	}
	if r.Pattern == "" {
		return fmt.Errorf("rule pattern is required")
	}
	if r.Depth < 1 {
		return fmt.Errorf("invalid rule depth %d (expected >= 1)", r.Depth)
	}
	if strings.HasPrefix(r.Pattern, regexPrefix) {
		re, err := regexp.Compile(strings.TrimPrefix(r.Pattern, regexPrefix))
		if err != nil {
			return fmt.Errorf("invalid regex pattern %q: %w", r.Pattern, err)
		}
		r.re = re
		return nil
	}
	if !doublestar.ValidatePattern(r.Pattern) {
		return fmt.Errorf("invalid glob pattern %q", r.Pattern)
	}
	return nil
}

func (r *Rule) matches(rel string) bool {
	rel = filepath.ToSlash(rel)
	if r.re != nil {
		return r.re.MatchString(rel)
	}
	matched, err := doublestar.Match(r.Pattern, rel)
	return err == nil && matched
}

// depthLimit returns the effective depth, treating an unset zero value as
// the default 1 (compiled rules always carry >= 1).
func (r Rule) depthLimit() int {
	if r.Depth < 1 {
		return 1
	}
	return r.Depth
}

// ValidateRules compiles every rule whose pattern is literal. Patterns
// containing expressions are skipped here; CompileRules validates them at
// runtime once evaluated.
func ValidateRules(rules []Rule) error {
	for i := range rules {
		if strings.Contains(rules[i].Pattern, exprOpen) {
			continue
		}
		if err := rules[i].compile(); err != nil {
			return err
		}
	}
	return nil
}

// RuleSet is a compiled list of rules; matching is OR within the list.
type RuleSet []Rule

// CompileRules validates and compiles every rule for matching.
func CompileRules(rules []Rule) (RuleSet, error) {
	set := make(RuleSet, len(rules))
	for i := range rules {
		set[i] = rules[i]
		if err := set[i].compile(); err != nil {
			return nil, err
		}
	}
	return set, nil
}

// MatchCandidate is the selection level: a rule matches the candidate iff
// the candidate level (path segments, 1 = direct child of the enumerated
// root) does not exceed the rule depth, its type covers the candidate kind
// (any covers both) and its pattern matches rel.
func (rs RuleSet) MatchCandidate(isDir bool, rel string) bool {
	rel = filepath.ToSlash(rel)
	level := strings.Count(rel, "/") + 1
	for i := range rs {
		r := &rs[i]
		if level > r.depthLimit() {
			continue
		}
		if r.Type != "" && r.Type != "any" && (r.Type == "dir") != isDir {
			continue
		}
		if r.matches(rel) {
			return true
		}
	}
	return false
}

// MatchContent is the content level, rel being a file's path relative to the
// copied root: file/any rules match the file itself, dir/any rules match any
// ancestor directory of it (pruning the whole subtree). Depth is
// selection-level only and ignored here: literal exclude rules carrying
// depth are rejected at load time, deferred ones simply lose it.
func (rs RuleSet) MatchContent(rel string) bool {
	rel = filepath.ToSlash(rel)
	for i := range rs {
		r := &rs[i]
		if r.Type != "dir" && r.matches(rel) {
			return true
		}
		if r.Type == "file" {
			continue
		}
		for parent := path.Dir(rel); parent != "."; parent = path.Dir(parent) {
			if r.matches(parent) {
				return true
			}
		}
	}
	return false
}
