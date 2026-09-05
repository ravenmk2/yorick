package declarative

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/sirupsen/logrus"
	"yorick/utils"
)

const (
	exprOpen  = "${{"
	exprClose = "}}"
)

// ExprScope is the evaluation environment for ${{ }} expressions. The expr
// tags define the visible names; unknown top-level names fail at compile
// time. Pure functions are func-typed fields because expr-lang does not
// honor expr tags on methods.
type ExprScope struct {
	Vars  map[string]any    `expr:"vars"`
	Env   map[string]string `expr:"env"`
	Os    string            `expr:"os"`
	Steps map[string]any    `expr:"steps"`

	IsDir     func(string) bool           `expr:"isDir"`
	IsFile    func(string) bool           `expr:"isFile"`
	FileExt   func(string) string         `expr:"fileExt"`
	AbsPath   func(string) string         `expr:"absPath"`
	IsAbsPath func(string) bool           `expr:"isAbsPath"`
	Format    func(string, ...any) string `expr:"format"`
}

func NewExprScope(vars map[string]any, envMap map[string]string) *ExprScope {
	logger := logrus.StandardLogger()
	return &ExprScope{
		Vars:  vars,
		Env:   envMap,
		Os:    runtime.GOOS,
		Steps: map[string]any{},
		IsDir: func(name string) bool {
			isDir, err := utils.IsDir(name)
			if err != nil {
				logger.Warnf("isDir: %s", err.Error())
				return false
			}
			return isDir
		},
		IsFile: func(name string) bool {
			isFile, err := utils.IsFile(name)
			if err != nil {
				logger.Warnf("isFile: %s", err.Error())
				return false
			}
			return isFile
		},
		FileExt:   filepath.Ext,
		IsAbsPath: filepath.IsAbs,
		AbsPath: func(path string) string {
			abs, err := filepath.Abs(path)
			if err != nil {
				logger.Warnf("absPath: %s", err.Error())
				return path
			}
			return abs
		},
		Format: fmt.Sprintf,
	}
}

func buildEnvMap() map[string]string {
	envMap := map[string]string{}
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		envMap[name] = value
	}
	return envMap
}

// scanExpr extracts the inner text of every ${{ }} occurrence in s and
// reports whether s is exactly one whole-string expression.
func scanExpr(s string) (inners []string, whole bool, err error) {
	pos := 0
	open0 := -1
	end0 := -1
	for {
		rel := strings.Index(s[pos:], exprOpen)
		if rel < 0 {
			break
		}
		open := pos + rel
		body := s[open+len(exprOpen):]
		rel = strings.Index(body, exprClose)
		if rel < 0 {
			return nil, false, fmt.Errorf("unclosed expression in %q", s)
		}
		inners = append(inners, body[:rel])
		end := open + len(exprOpen) + rel + len(exprClose)
		if open0 < 0 {
			open0, end0 = open, end
		}
		pos = end
	}
	whole = len(inners) == 1 && open0 == 0 && end0 == len(s)
	return inners, whole, nil
}

// exprCache holds programs compiled once at load time, keyed by expression
// text plus mode ("a:" keeps the raw value, "b:" is compiled AsBool).
type exprCache map[string]*vm.Program

func exprKey(inner string, asBool bool) string {
	if asBool {
		return "b:" + inner
	}
	return "a:" + inner
}

func (c exprCache) compileInner(inner string, asBool bool) error {
	key := exprKey(inner, asBool)
	if _, ok := c[key]; ok {
		return nil
	}
	opts := []expr.Option{expr.Env(ExprScope{})}
	if asBool {
		opts = append(opts, expr.AsBool())
	}
	program, err := expr.Compile(inner, opts...)
	if err != nil {
		return fmt.Errorf("compile expression %q: %w", strings.TrimSpace(inner), err)
	}
	c[key] = program
	return nil
}

// scanValue returns the inner text of all expressions in strings under value.
func scanValue(value any) (inners []string, err error) {
	switch v := value.(type) {
	case string:
		found, _, err := scanExpr(v)
		if err != nil {
			return nil, err
		}
		inners = append(inners, found...)
	case map[string]any:
		for _, item := range v {
			found, err := scanValue(item)
			if err != nil {
				return nil, err
			}
			inners = append(inners, found...)
		}
	case []any:
		for _, item := range v {
			found, err := scanValue(item)
			if err != nil {
				return nil, err
			}
			inners = append(inners, found...)
		}
	}
	return inners, nil
}

func (c exprCache) evalInner(inner string, asBool bool, scope *ExprScope) (any, error) {
	key := exprKey(inner, asBool)
	program, ok := c[key]
	if !ok {
		if err := c.compileInner(inner, asBool); err != nil {
			return nil, err
		}
		program = c[key]
	}
	return vm.Run(program, *scope)
}

func (c exprCache) evalBool(source string, scope *ExprScope) (bool, error) {
	inners, _, err := scanExpr(source)
	if err != nil {
		return false, err
	}
	value, err := c.evalInner(inners[0], true, scope)
	if err != nil {
		return false, err
	}
	return value.(bool), nil
}

// interpolate resolves expressions recursively; non-string values pass
// through untouched. A whole-string expression keeps its raw typed result,
// embedded occurrences are stringify-concatenated.
func (c exprCache) interpolate(value any, scope *ExprScope) (any, error) {
	switch v := value.(type) {
	case string:
		return c.interpolateString(v, scope)
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			interpolated, err := c.interpolate(item, scope)
			if err != nil {
				return nil, err
			}
			result[key] = interpolated
		}
		return result, nil
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			interpolated, err := c.interpolate(item, scope)
			if err != nil {
				return nil, err
			}
			result[i] = interpolated
		}
		return result, nil
	default:
		return value, nil
	}
}

func (c exprCache) interpolateString(s string, scope *ExprScope) (any, error) {
	inners, whole, err := scanExpr(s)
	if err != nil {
		return nil, err
	}
	if len(inners) == 0 {
		return s, nil
	}
	if whole {
		return c.evalInner(inners[0], false, scope)
	}

	var sb strings.Builder
	pos := 0
	for _, inner := range inners {
		open := pos + strings.Index(s[pos:], exprOpen)
		sb.WriteString(s[pos:open])
		value, err := c.evalInner(inner, false, scope)
		if err != nil {
			return nil, err
		}
		fmt.Fprint(&sb, value)
		pos = open + len(exprOpen) + len(inner) + len(exprClose)
	}
	sb.WriteString(s[pos:])
	return sb.String(), nil
}
