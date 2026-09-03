package core

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v3"
	"yorick/utils"
)

type StepContext struct {
	logger  *logrus.Logger
	destDir string
}

// resolveDest resolves a relative dest arg under the task dest dir.
func (c *StepContext) resolveDest(dest string) string {
	if filepath.IsAbs(dest) {
		return dest
	}
	return filepath.Join(c.destDir, dest)
}

type copyArgs struct {
	Src     string   `yaml:"src"`
	Dest    string   `yaml:"dest"`
	Exclude []string `yaml:"exclude,omitempty"`
}

type collectArgs struct {
	Dir     string   `yaml:"dir"`
	Depth   int      `yaml:"depth,omitempty"`
	Type    string   `yaml:"type,omitempty"`
	Include []string `yaml:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
}

type readIniArgs struct {
	File string `yaml:"file"`
	Expr string `yaml:"expr"`
}

type latestFileArgs struct {
	Dir   string `yaml:"dir"`
	Depth int   `yaml:"depth,omitempty"`
}

type regExportArgs struct {
	Key  string `yaml:"key"`
	Dest string `yaml:"dest"`
}

type hostsFileArgs struct {
	Dest string `yaml:"dest,omitempty"`
}

type logArgs struct {
	Msg string `yaml:"msg"`
}

type execArgs struct {
	Cmd    string `yaml:"cmd"`
	Stdout string `yaml:"stdout,omitempty"`
}

type stepEntry struct {
	argsType reflect.Type
	run      func(ctx *StepContext, args map[string]any) (any, error)
}

func newStepEntry[T any](run func(ctx *StepContext, args *T) (any, error)) *stepEntry {
	return &stepEntry{
		argsType: reflect.TypeOf((*T)(nil)).Elem(),
		run: func(ctx *StepContext, raw map[string]any) (any, error) {
			args := new(T)
			if err := decodeStepArgs(raw, args); err != nil {
				return nil, err
			}
			return run(ctx, args)
		},
	}
}

// decodeStepArgs decodes interpolated args strictly into the func's arg struct.
func decodeStepArgs(raw map[string]any, out any) error {
	data, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(out)
}

var stepFuncs = map[string]*stepEntry{
	"copy":        newStepEntry[copyArgs](runCopy),
	"collect":     newStepEntry[collectArgs](runCollect),
	"read-ini":    newStepEntry[readIniArgs](runReadIni),
	"latest-file": newStepEntry[latestFileArgs](runLatestFile),
	"reg-export":  newStepEntry[regExportArgs](runRegExport),
	"hosts-file":  newStepEntry[hostsFileArgs](runHostsFile),
	"log":         newStepEntry[logArgs](runLog),
	"exec":        newStepEntry[execArgs](runExec),
}

func runCopy(ctx *StepContext, args *copyArgs) (any, error) {
	ctx.logger.Infof("Copy: %s => %s", args.Src, args.Dest)

	src, err := utils.ExpandUser(args.Src)
	if err != nil {
		return nil, err
	}
	dest := ctx.resolveDest(args.Dest)

	isFile, err := utils.IsFile(src)
	if err != nil {
		return nil, err
	}
	if isFile {
		return nil, utils.SafeCopyFile(src, dest)
	}

	isDir, err := utils.IsDir(src)
	if err != nil {
		return nil, err
	}
	if !isDir {
		ctx.logger.Errorf("Invalid source: %s", src)
		return nil, nil
	}

	excludes, err := CompilePatternList(args.Exclude)
	if err != nil {
		return nil, err
	}
	files, err := utils.ListFiles(src, true, -1)
	if err != nil {
		return nil, err
	}
	for _, file := range FilterCandidates(files, nil, excludes) {
		err := utils.SafeCopyFile(filepath.Join(src, file), filepath.Join(dest, file))
		if err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func runCollect(ctx *StepContext, args *collectArgs) (any, error) {
	dir, err := utils.ExpandUser(args.Dir)
	if err != nil {
		return nil, err
	}
	depth := args.Depth
	if depth == 0 {
		depth = 1
	}
	collectType := args.Type
	if collectType == "" {
		collectType = "any"
	}
	if collectType != "dir" && collectType != "file" && collectType != "any" {
		return nil, fmt.Errorf("invalid type %q (expected dir, file or any)", collectType)
	}
	ctx.logger.Infof("Collect: %s (type=%s depth=%d)", dir, collectType, depth)

	includes, err := CompilePatternList(args.Include)
	if err != nil {
		return nil, err
	}
	excludes, err := CompilePatternList(args.Exclude)
	if err != nil {
		return nil, err
	}

	type candidate struct {
		rel   string
		isDir bool
	}
	candidates := []candidate{}
	if collectType != "file" {
		dirs, err := utils.ListDirs(dir, true, depth)
		if err != nil {
			return nil, err
		}
		for _, rel := range dirs {
			candidates = append(candidates, candidate{rel, true})
		}
	}
	if collectType != "dir" {
		files, err := utils.ListFiles(dir, true, depth)
		if err != nil {
			return nil, err
		}
		for _, rel := range files {
			candidates = append(candidates, candidate{rel, false})
		}
	}

	output := []any{}
	for _, c := range candidates {
		if len(includes) > 0 && !includes.MatchesAny(c.rel) {
			continue
		}
		if excludes.MatchesAny(c.rel) {
			continue
		}
		output = append(output, newFileInfo(dir, c.rel, c.isDir))
	}
	ctx.logger.Infof("Collect: %d matched", len(output))
	return output, nil
}

// newFileInfo builds the {name, path, rel, ext} item shape shared by
// collect and latest-file.
func newFileInfo(dir, rel string, isDir bool) map[string]any {
	name := filepath.Base(rel)
	ext := ""
	if !isDir {
		ext = filepath.Ext(name)
	}
	return map[string]any{
		"name": name,
		"path": filepath.Join(dir, rel),
		"rel":  rel,
		"ext":  ext,
	}
}

func runReadIni(ctx *StepContext, args *readIniArgs) (any, error) {
	ctx.logger.Infof("ReadIni: %s (%s)", args.File, args.Expr)

	file, err := utils.ExpandUser(args.File)
	if err != nil {
		return nil, err
	}
	iniFile, err := ini.Load(file)
	if err != nil {
		return nil, err
	}

	sections := []*ini.Section{}
	for _, section := range iniFile.Sections() {
		if section.Name() == ini.DefaultSection {
			continue
		}
		sections = append(sections, section)
	}

	segments := strings.Split(args.Expr, ".")
	var current *ini.Section
	for i, segment := range segments {
		last := i == len(segments)-1
		if len(segment) > 2 && strings.HasPrefix(segment, "[") && strings.HasSuffix(segment, "]") {
			index, err := strconv.Atoi(segment[1 : len(segment)-1])
			if err != nil {
				return nil, fmt.Errorf("malformed ini expression %q: bad section index %q", args.Expr, segment)
			}
			if index < 0 || index >= len(sections) {
				return nil, fmt.Errorf("ini expression %q: section index %d out of range (%d sections)", args.Expr, index, len(sections))
			}
			current = sections[index]
			continue
		}
		if !last {
			section, err := iniFile.GetSection(segment)
			if err != nil {
				return nil, fmt.Errorf("ini expression %q: section %q not found", args.Expr, segment)
			}
			current = section
			continue
		}
		if current == nil {
			return nil, fmt.Errorf("malformed ini expression %q: no section selected", args.Expr)
		}
		key, err := current.GetKey(segment)
		if err != nil {
			return nil, fmt.Errorf("ini expression %q: key %q not found in section %q", args.Expr, segment, current.Name())
		}
		return key.Value(), nil
	}
	return nil, fmt.Errorf("malformed ini expression %q: no key selected", args.Expr)
}

func runLatestFile(ctx *StepContext, args *latestFileArgs) (any, error) {
	ctx.logger.Infof("LatestFile: %s", args.Dir)

	dir, err := utils.ExpandUser(args.Dir)
	if err != nil {
		return nil, err
	}
	depth := args.Depth
	if depth == 0 {
		depth = 1
	}
	rel, err := utils.FindLatestFile(dir, true, depth)
	if err != nil {
		return nil, err
	}
	if rel == "" {
		return nil, fmt.Errorf("no files found in %s", dir)
	}
	return newFileInfo(dir, rel, false), nil
}

func runRegExport(ctx *StepContext, args *regExportArgs) (any, error) {
	key := strings.ReplaceAll(args.Key, `/`, `\`)
	ctx.logger.Infof("RegExport: %s => %s", key, args.Dest)

	if runtime.GOOS != "windows" {
		ctx.logger.Info("RegExport: skipped (windows only)")
		return nil, nil
	}

	dest := ctx.resolveDest(args.Dest)
	if err := utils.MakeParentDir(dest); err != nil {
		return nil, err
	}

	cmd := exec.Command("reg", "export", key, dest, "/y")
	if err := cmd.Run(); err != nil {
		ctx.logger.Errorf("reg export error: %s", err.Error())
	}
	return nil, nil
}

func runHostsFile(ctx *StepContext, args *hostsFileArgs) (any, error) {
	dest := args.Dest
	if dest == "" {
		dest = "hosts"
	}
	ctx.logger.Infof("HostsFile: %s => %s", utils.HostsFilePath, dest)
	return nil, utils.SafeCopyFile(utils.HostsFilePath, ctx.resolveDest(dest))
}

func runLog(ctx *StepContext, args *logArgs) (any, error) {
	ctx.logger.Info(args.Msg)
	return nil, nil
}

func runExec(ctx *StepContext, args *execArgs) (any, error) {
	ctx.logger.Infof("Exec: %s", args.Cmd)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", args.Cmd)
	} else {
		cmd = exec.Command("sh", "-c", args.Cmd)
	}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		ctx.logger.Errorf("Exec: %s", err.Error())
		return nil, nil
	}

	if args.Stdout != "" {
		dest := ctx.resolveDest(args.Stdout)
		if err := utils.MakeParentDir(dest); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, stdout.Bytes(), 0o644); err != nil {
			return nil, err
		}
	} else if stdout.Len() > 0 {
		ctx.logger.Info(strings.TrimRight(stdout.String(), "\r\n"))
	}
	return nil, nil
}
