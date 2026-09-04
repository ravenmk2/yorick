package core

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const SpecVersion = 1

type Spec struct {
	Version int            `yaml:"version"`
	Name    string         `yaml:"name,omitempty"`
	Vars    map[string]any `yaml:"vars,omitempty"`
	Tasks   []*TaskSpec    `yaml:"tasks"`

	programs exprCache
}

type TaskSpec struct {
	Name  string      `yaml:"name"`
	Dest  string      `yaml:"dest"`
	If    string      `yaml:"if,omitempty"`
	Steps []*StepSpec `yaml:"steps"`
}

type StepSpec struct {
	Id   string         `yaml:"id,omitempty"`
	Func string         `yaml:"func"`
	If   string         `yaml:"if,omitempty"`
	Args map[string]any `yaml:"args,omitempty"`
}

var stepRefRegex = regexp.MustCompile(`steps\.([A-Za-z_][A-Za-z0-9_-]*)\.output`)

// ruleArgs lists, per func, the args holding rule lists so literal rules
// can be validated at load time.
var ruleArgs = map[string][]string{
	"copy": {"include", "exclude"},
}

func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	spec := &Spec{programs: exprCache{}}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if spec.Version != SpecVersion {
		return nil, fmt.Errorf("unsupported version %d (expected %d)", spec.Version, SpecVersion)
	}
	if len(spec.Tasks) == 0 {
		return nil, fmt.Errorf("no tasks defined")
	}
	if err := checkStaticVars(spec.Vars); err != nil {
		return nil, err
	}

	for i, task := range spec.Tasks {
		if err := spec.compileTask(i, task); err != nil {
			return nil, err
		}
	}
	return spec, nil
}

// checkStaticVars rejects expressions in vars: they hold static values only.
func checkStaticVars(vars map[string]any) error {
	inners, err := scanValue(vars)
	if err != nil {
		return fmt.Errorf("vars: %w", err)
	}
	if len(inners) > 0 {
		return fmt.Errorf("vars: expressions are not allowed, values must be static")
	}
	return nil
}

func taskWhere(index int, task *TaskSpec) string {
	if task.Name != "" {
		return fmt.Sprintf("task %q", task.Name)
	}
	return fmt.Sprintf("task %d", index+1)
}

func stepWhere(task *TaskSpec, index int, step *StepSpec) string {
	where := fmt.Sprintf("task %q step %d", task.Name, index+1)
	if step.Id != "" {
		where += fmt.Sprintf(" (%s)", step.Id)
	}
	return where
}

func (s *Spec) compileTask(index int, task *TaskSpec) error {
	where := taskWhere(index, task)
	if task.Name == "" {
		return fmt.Errorf("%s: name is required", where)
	}
	if task.Dest == "" {
		return fmt.Errorf("%s: dest is required", where)
	}
	if len(task.Steps) == 0 {
		return fmt.Errorf("%s: no steps defined", where)
	}

	ids := map[string]bool{}
	if task.If != "" {
		if err := s.compileCondition(task.If, ids, fmt.Sprintf("%s if", task.Name)); err != nil {
			return err
		}
	}

	for j, step := range task.Steps {
		stepWhere := stepWhere(task, j, step)

		entry, ok := stepFuncs[step.Func]
		if !ok {
			return fmt.Errorf("%s: unknown func %q", stepWhere, step.Func)
		}
		if err := validateStepArgKeys(entry, step.Args, stepWhere); err != nil {
			return err
		}
		if step.If != "" {
			if err := s.compileCondition(step.If, ids, stepWhere+" if"); err != nil {
				return err
			}
		}
		if err := s.compileStepArgs(step, ids, stepWhere); err != nil {
			return err
		}
		if err := validateStepRules(step, stepWhere); err != nil {
			return err
		}

		if step.Id != "" {
			if ids[step.Id] {
				return fmt.Errorf("%s: duplicate step id %q", stepWhere, step.Id)
			}
			ids[step.Id] = true
		}
	}
	return nil
}

// compileCondition validates a task/step if value: it must be one
// whole-string expression evaluating to bool.
func (s *Spec) compileCondition(source string, ids map[string]bool, where string) error {
	inners, whole, err := scanExpr(source)
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	if !whole {
		return fmt.Errorf("%s: must be a single ${{ }} expression: %q", where, source)
	}
	if err := validateStepRefs(inners[0], ids, where); err != nil {
		return err
	}
	if err := s.programs.compileInner(inners[0], true); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	return nil
}

func (s *Spec) compileStepArgs(step *StepSpec, ids map[string]bool, where string) error {
	return walkStrings(step.Args, func(raw string) error {
		inners, _, err := scanExpr(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		for _, inner := range inners {
			if err := validateStepRefs(inner, ids, where); err != nil {
				return err
			}
			if err := s.programs.compileInner(inner, false); err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
		}
		return nil
	})
}

// walkStrings calls fn for every string value found under value.
func walkStrings(value any, fn func(string) error) error {
	switch v := value.(type) {
	case string:
		return fn(v)
	case map[string]any:
		for _, item := range v {
			if err := walkStrings(item, fn); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range v {
			if err := walkStrings(item, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateStepRefs checks that every steps.<id>.output reference points to
// a step declared earlier in the same task (ids holds earlier ids only).
func validateStepRefs(inner string, ids map[string]bool, where string) error {
	for _, match := range stepRefRegex.FindAllStringSubmatch(inner, -1) {
		if !ids[match[1]] {
			return fmt.Errorf("%s: steps.%s.output does not reference an earlier step in the same task", where, match[1])
		}
	}
	return nil
}

// validateStepArgKeys checks arg keys against the func's arg struct: a yaml
// field without omitempty is required, unknown keys are rejected.
func validateStepArgKeys(entry *stepEntry, args map[string]any, where string) error {
	known := map[string]bool{}
	required := []string{}
	argsType := entry.argsType
	for i := 0; i < argsType.NumField(); i++ {
		tag := argsType.Field(i).Tag.Get("yaml")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		known[name] = true
		if !strings.Contains(opts, "omitempty") {
			required = append(required, name)
		}
	}

	for key := range args {
		if !known[key] {
			return fmt.Errorf("%s: unknown arg %q", where, key)
		}
	}
	for _, name := range required {
		if _, ok := args[name]; !ok {
			return fmt.Errorf("%s: missing required arg %q", where, name)
		}
	}
	return nil
}

// validateStepRules validates literal include/exclude rules (both the
// scalar shorthand and the {type, pattern} mapping form); rules whose
// pattern contains an expression are skipped (resolved at run time). Depth
// is include-only: a literal exclude rule carrying depth fails here,
// deferred ones are tolerated (MatchContent ignores depth at runtime).
func validateStepRules(step *StepSpec, where string) error {
	for _, key := range ruleArgs[step.Func] {
		list, ok := step.Args[key].([]any)
		if !ok {
			continue
		}
		rules := make([]Rule, 0, len(list))
		for i, item := range list {
			if key == "exclude" {
				if m, ok := item.(map[string]any); ok {
					if d, ok := m["depth"]; ok && !strings.Contains(fmt.Sprint(d), exprOpen) {
						return fmt.Errorf("%s: %s[%d]: depth is include-only, not allowed on exclude rules", where, key, i)
					}
				}
			}
			data, err := yaml.Marshal(item)
			if err != nil {
				return err
			}
			var rule Rule
			if err := yaml.Unmarshal(data, &rule); err != nil {
				return fmt.Errorf("%s: %s: %w", where, key, err)
			}
			rules = append(rules, rule)
		}
		if err := ValidateRules(rules); err != nil {
			return fmt.Errorf("%s: %s: %w", where, key, err)
		}
	}
	return nil
}
