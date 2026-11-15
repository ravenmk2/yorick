package scripted

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/grafana/sobek"
	"github.com/sirupsen/logrus"
	"yorick/utils"
)

const (
	RegKeySystemEnv = `HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
	RegKeyUserEnv   = `HKEY_CURRENT_USER\Environment`
)

type ScriptObject struct {
	logger     *logrus.Logger
	vm         *sobek.Runtime
	outputDir  string
	subDir     string
	currentDir string
	tasks      []*TaskInfo
	rejections []*sobek.Promise
}

func NewScriptObject(vm *sobek.Runtime, outputDir string) *ScriptObject {
	s := &ScriptObject{
		logger:     logrus.StandardLogger(),
		vm:         vm,
		outputDir:  outputDir,
		subDir:     ".",
		currentDir: outputDir,
		tasks:      []*TaskInfo{},
	}
	vm.SetPromiseRejectionTracker(func(p *sobek.Promise, operation sobek.PromiseRejectionOperation) {
		switch operation {
		case sobek.PromiseRejectionReject:
			s.rejections = append(s.rejections, p)
		case sobek.PromiseRejectionHandle:
			s.rejections = slices.DeleteFunc(s.rejections, func(rejected *sobek.Promise) bool { return rejected == p })
		}
	})
	return s
}

func (s *ScriptObject) RunScript(name, source string) error {
	result, err := s.vm.RunScript(name, source)
	if err != nil {
		return err
	}
	if err := s.checkSyncResult(result); err != nil {
		return fmt.Errorf("job %q: %w", name, err)
	}
	return nil
}

func (s *ScriptObject) checkSyncResult(result sobek.Value) error {
	// Sobek drains microtasks before returning; a rejection may have been handled since it was reported.
	if len(s.rejections) > 0 {
		return fmt.Errorf("unhandled Promise rejection: %v (jobs must be synchronous)", s.rejections[0].Result())
	}
	// Inspect the type without exporting arbitrary objects, which could invoke getters.
	if result != nil && result.ExportType() == reflect.TypeFor[*sobek.Promise]() {
		return fmt.Errorf("Promise results are not supported; jobs and task callbacks must be synchronous")
	}
	return nil
}

func (s *ScriptObject) DefineTask(name string, value sobek.Value) {
	fn, ok := sobek.AssertFunction(value)
	if !ok {
		panic(s.vm.NewTypeError("task %q: callback must be a function", name))
	}
	task := &TaskInfo{
		Name: name,
		dir:  name,
		fn:   fn,
	}
	s.tasks = append(s.tasks, task)
}

func (s *ScriptObject) ExecTasks() error {
	count := len(s.tasks)
	for i, task := range s.tasks {
		n := i + 1
		s.logger.Infof("[%d/%d] Task: %s", n, count, task.Name)
		c := NewTaskContext(task)
		result, err := task.fn(sobek.Null(), s.vm.ToValue(c))
		if err != nil {
			return fmt.Errorf("task %q: %w", task.Name, err)
		}
		if err := s.checkSyncResult(result); err != nil {
			return fmt.Errorf("task %q: %w", task.Name, err)
		}
	}
	return nil
}

func (s *ScriptObject) DestDir(dir string) {
	s.subDir = dir
	s.currentDir = filepath.Join(s.outputDir, dir)
	s.logger.Infof("Destination: %s", s.currentDir)
}

func (s *ScriptObject) CopyFile(srcFile, dstFile string) {
	s.logger.Infof("CopyFile: %s => %s", srcFile, filepath.Join(s.subDir, dstFile))

	srcFile, err := utils.ExpandUser(srcFile)
	if err != nil {
		s.logger.Fatal(err)
	}

	isFile, err := utils.IsFile(srcFile)
	if err != nil {
		s.logger.Fatal(err)
	}
	if !isFile {
		s.logger.Errorf("Invalid source file: %s", srcFile)
		return
	}

	dstFile = filepath.Join(s.currentDir, dstFile)
	err = utils.SafeCopyFile(srcFile, dstFile)
	if err != nil {
		s.logger.Fatal(err)
	}
}

func (s *ScriptObject) CopyDir(srcDir, dstDir string) {
	s.logger.Infof("CopyDir: %s => %s", srcDir, filepath.Join(s.subDir, dstDir))

	srcDir, err := utils.ExpandUser(srcDir)
	if err != nil {
		s.logger.Fatal(err)
	}

	isDir, err := utils.IsDir(srcDir)
	if err != nil {
		s.logger.Fatal(err)
	}
	if !isDir {
		s.logger.Errorf("Invalid source dirctory: %s", srcDir)
		return
	}

	dstDir = filepath.Join(s.currentDir, dstDir)
	err = utils.SafeCopyDir(srcDir, dstDir)
	if err != nil {
		s.logger.Fatal(err)
	}
}

func (s *ScriptObject) CopyDirEx(srcDir, dstDir string, excludes []string) {
	s.logger.Infof("CopyDirEx: %s => %s", srcDir, filepath.Join(s.subDir, dstDir))

	srcDir, err := utils.ExpandUser(srcDir)
	if err != nil {
		s.logger.Fatal(err)
	}

	isDir, err := utils.IsDir(srcDir)
	if err != nil {
		s.logger.Fatal(err)
	}
	if !isDir {
		s.logger.Errorf("Invalid source dirctory: %s", srcDir)
		return
	}

	dstDir = filepath.Join(s.currentDir, dstDir)
	err = utils.SafeCopyDirEx(srcDir, dstDir, excludes)
	if err != nil {
		s.logger.Fatal(err)
	}
}

func (s *ScriptObject) ExportRegistry(key, dstFile string) {
	key = strings.ReplaceAll(key, `/`, `\`)
	s.logger.Infof("ExportRegistry: %s => %s", key, filepath.Join(s.subDir, dstFile))

	dstFile = filepath.Join(s.currentDir, dstFile)
	err := utils.MakeParentDir(dstFile)
	if err != nil {
		s.logger.Fatal(err)
	}

	cmd := exec.Command("reg", "export", key, dstFile, "/y")
	err = cmd.Run()
	if err != nil {
		s.logger.Errorf("reg export error: %s", err.Error())
	}
}

func (s *ScriptObject) PutHostsFile() {
	srcFile := utils.HostsFilePath
	dstFile := "hosts"
	s.logger.Infof("PutHostsFile: %s => %s", srcFile, dstFile)
	dstFile = filepath.Join(s.currentDir, dstFile)
	err := utils.SafeCopyFile(srcFile, dstFile)
	if err != nil {
		s.logger.Fatal(err)
	}
}

func (s *ScriptObject) ExportRegSystemEnv(dstFile string) {
	s.ExportRegistry(RegKeySystemEnv, dstFile)
}

func (s *ScriptObject) ExportRegUserEnv(dstFile string) {
	s.ExportRegistry(RegKeyUserEnv, dstFile)
}

func (s *ScriptObject) RegisterFuncs() error {
	funcs := map[string]any{
		"task":               s.DefineTask,
		"destDir":            s.DestDir,
		"copyFile":           s.CopyFile,
		"copyDir":            s.CopyDir,
		"copyDirEx":          s.CopyDirEx,
		"exportRegistry":     s.ExportRegistry,
		"exportReg":          s.ExportRegistry,
		"putHostsFile":       s.PutHostsFile,
		"exportRegSystemEnv": s.ExportRegSystemEnv,
		"exportRegUserEnv":   s.ExportRegUserEnv,
	}

	for name, fn := range funcs {
		err := s.vm.Set(name, fn)
		if err != nil {
			return err
		}
	}
	return nil
}
