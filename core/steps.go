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
	Src     string `yaml:"src"`
	Dest    string `yaml:"dest"`
	Include []Rule `yaml:"include,omitempty"`
	Exclude []Rule `yaml:"exclude,omitempty"`
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
	"read-ini":    newStepEntry[readIniArgs](runReadIni),
	"latest-file": newStepEntry[latestFileArgs](runLatestFile),
	"reg-export":  newStepEntry[regExportArgs](runRegExport),
	"hosts-file":  newStepEntry[hostsFileArgs](runHostsFile),
	"log":         newStepEntry[logArgs](runLog),
	"exec":        newStepEntry[execArgs](runExec),
}

func runCopy(ctx *StepContext, args *copyArgs) (any, error) {
	src, err := utils.ExpandUser(args.Src)
	if err != nil {
		return nil, err
	}

	// A non-empty include switches to enumeration mode: src is a container,
	// each selected child is copied to dest/<base name>.
	if len(args.Include) > 0 {
		return nil, runCopyEnumerate(ctx, args, src)
	}

	ctx.logger.Infof("Copy: %s => %s", args.Src, args.Dest)
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

	excludes, err := CompileRules(args.Exclude)
	if err != nil {
		return nil, err
	}
	files, err := utils.ListFiles(src, true, -1)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if excludes.MatchContent(file) {
			continue
		}
		if err := utils.SafeCopyFile(filepath.Join(src, file), filepath.Join(dest, file)); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func runCopyEnumerate(ctx *StepContext, args *copyArgs, src string) error {
	isDir, err := utils.IsDir(src)
	if err != nil {
		return err
	}
	if !isDir {
		return fmt.Errorf("copy: %s is not a directory (include enumerates a container)", src)
	}

	includes, err := CompileRules(args.Include)
	if err != nil {
		return err
	}
	excludes, err := CompileRules(args.Exclude)
	if err != nil {
		return err
	}

	// Walk as deep as the deepest include rule (rule depths default to 1).
	depth := 1
	for i := range includes {
		if d := includes[i].depthLimit(); d > depth {
			depth = d
		}
	}

	type candidate struct {
		rel   string
		isDir bool
	}
	candidates := []candidate{}
	dirs, err := utils.ListDirs(src, true, depth)
	if err != nil {
		return err
	}
	for _, rel := range dirs {
		candidates = append(candidates, candidate{rel, true})
	}
	files, err := utils.ListFiles(src, true, depth)
	if err != nil {
		return err
	}
	for _, rel := range files {
		candidates = append(candidates, candidate{rel, false})
	}

	matched := []candidate{}
	for _, c := range candidates {
		if len(includes) > 0 && !includes.MatchCandidate(c.isDir, c.rel) {
			continue
		}
		matched = append(matched, c)
	}

	destDir := ctx.resolveDest(args.Dest)
	ctx.logger.Infof("Copy: matched %d items", len(matched))
	for _, c := range matched {
		abs := filepath.Join(src, c.rel)
		target := filepath.Join(destDir, filepath.Base(c.rel))
		ctx.logger.Infof("Copy: %s => %s", c.rel, target)
		if !c.isDir {
			if err := utils.SafeCopyFile(abs, target); err != nil {
				return err
			}
			continue
		}
		files, err := utils.ListFiles(abs, true, -1)
		if err != nil {
			return err
		}
		for _, file := range files {
			if excludes.MatchContent(file) {
				continue
			}
			if err := utils.SafeCopyFile(filepath.Join(abs, file), filepath.Join(target, file)); err != nil {
				return err
			}
		}
	}
	return nil
}

// newFileInfo builds the {name, path, rel, ext} output shape of latest-file.
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
