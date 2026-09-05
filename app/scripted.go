package app

import (
	"os"

	"github.com/robertkrimen/otto"
	"github.com/sirupsen/logrus"
	"yorick/scripted"
)

func RunScripted(jobFile, outputDir string) error {
	logrus.Infof("Job file: %s", jobFile)
	logrus.Infof("Output directory: %s", outputDir)

	content, err := os.ReadFile(jobFile)
	if err != nil {
		return err
	}
	source := string(content)

	vm := otto.New()
	fo := scripted.NewFunctionsObject(vm)
	so := scripted.NewScriptObject(vm, outputDir)

	err = fo.RegisterFuncs()
	if err != nil {
		return err
	}

	err = so.RegisterFuncs()
	if err != nil {
		return err
	}

	_, err = vm.Run(scripted.InitScript + source)
	if err != nil {
		return err
	}

	err = so.ExecTasks()
	if err != nil {
		return err
	}

	logrus.Info("All done")
	return nil
}
