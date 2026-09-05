package scripted

import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/robertkrimen/otto"
	"github.com/sirupsen/logrus"
	"yorick/utils"
)

const (
	RegKeySystemEnv = `HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
	RegKeyUserEnv   = `HKEY_CURRENT_USER\Environment`
)

type ScriptObject struct {
	logger     *logrus.Logger
	vm         *otto.Otto
	outputDir  string
	subDir     string
	currentDir string
	tasks      []*TaskInfo
}

func NewScriptObject(vm *otto.Otto, outputDir string) *ScriptObject {
	return &ScriptObject{
		logger:     logrus.StandardLogger(),
		vm:         vm,
		outputDir:  outputDir,
		subDir:     ".",
		currentDir: outputDir,
		tasks:      []*TaskInfo{},
	}
}

func (s *ScriptObject) DefineTask(name string, value *otto.Value) {
	task := &TaskInfo{
		Name: name,
		dir:  name,
		fn:   value,
	}
	s.tasks = append(s.tasks, task)
}

func (s *ScriptObject) ExecTasks() error {
	count := len(s.tasks)
	for i, task := range s.tasks {
		n := i + 1
		s.logger.Infof("[%d/%d] Task: %s", n, count, task.Name)
		c := NewTaskContext(task)
		_, err := task.fn.Call(otto.NullValue(), c)
		if err != nil {
			return err
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
