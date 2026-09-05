package scripted

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/robertkrimen/otto"
	"github.com/sirupsen/logrus"
	"yorick/utils"
)

type FunctionsObject struct {
	logger *logrus.Logger
	vm     *otto.Otto
}

func NewFunctionsObject(vm *otto.Otto) *FunctionsObject {
	return &FunctionsObject{
		logger: logrus.StandardLogger(),
		vm:     vm,
	}
}

func (o *FunctionsObject) Format(format string, a ...any) string {
	return fmt.Sprintf(format, a...)
}

func (o *FunctionsObject) LogInfo(format string, args ...any) {
	logrus.Infof(format, args...)
}

func (o *FunctionsObject) LogWarn(format string, args ...any) {
	logrus.Warnf(format, args...)
}

func (o *FunctionsObject) LogError(format string, args ...any) {
	logrus.Errorf(format, args...)
}

func (o *FunctionsObject) GetEnv(key string) string {
	return os.Getenv(key)
}

func (o *FunctionsObject) IsDir(name string) bool {
	isDir, err := utils.IsDir(name)
	if err != nil {
		o.logger.Warnf("IsDir: %s", err.Error())
		return false
	}
	return isDir
}

func (o *FunctionsObject) IsFile(name string) bool {
	isFile, err := utils.IsFile(name)
	if err != nil {
		o.logger.Warnf("IsFile: %s", err.Error())
		return false
	}
	return isFile
}

func (o *FunctionsObject) FileExt(path string) string {
	return filepath.Ext(path)
}

func (o *FunctionsObject) AbsPath(path string) string {
	path, err := filepath.Abs(path)
	if err != nil {
		o.logger.Fatal(err)
		return path
	}
	return path
}

func (o *FunctionsObject) IsAbsPath(path string) bool {
	return filepath.IsAbs(path)
}

func (o *FunctionsObject) ListDirs(dir string, relative bool, maxDepth int) []string {
	files, err := utils.ListDirs(dir, relative, maxDepth)
	if err != nil {
		o.logger.Warnf("ListDirs: %s", err.Error())
		return nil
	}
	return files
}

func (o *FunctionsObject) ListFiles(dir string, relative bool, maxDepth int) []string {
	files, err := utils.ListFiles(dir, relative, maxDepth)
	if err != nil {
		o.logger.Warnf("ListFiles: %s", err.Error())
		return nil
	}
	return files
}

func (o *FunctionsObject) FindLatestFile(dir string, relative bool, maxDepth int) string {
	file, err := utils.FindLatestFile(dir, relative, maxDepth)
	if err != nil {
		o.logger.Fatalf("FindLatestFile: %s", err.Error())
		return ""
	}
	return file
}

func (o *FunctionsObject) ReadIni(path string) utils.IniDict {
	dict, err := utils.ReadIniAsDict(path)
	if err != nil {
		o.logger.Fatalf("ReadIni: %s", err.Error())
	}
	return dict
}

func (o *FunctionsObject) RegisterFuncs() error {
	funcs := map[string]any{
		"format":         o.Format,
		"logInfo":        o.LogInfo,
		"logWarn":        o.LogWarn,
		"logError":       o.LogError,
		"getEnv":         o.GetEnv,
		"isDir":          o.IsDir,
		"isFile":         o.IsFile,
		"fileExt":        o.FileExt,
		"absPath":        o.AbsPath,
		"isAbsPath":      o.IsAbsPath,
		"listDirs":       o.ListDirs,
		"listFiles":      o.ListFiles,
		"findLatestFile": o.FindLatestFile,
		"readIni":        o.ReadIni,
	}

	for name, fn := range funcs {
		err := o.vm.Set(name, fn)
		if err != nil {
			return err
		}
	}
	return nil
}
