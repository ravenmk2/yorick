package app

import (
	"github.com/sirupsen/logrus"
	"yorick/core"
)

func ExecRunSpec(specFile, outputDir string) error {
	logrus.Infof("Job file: %s", specFile)

	spec, err := core.LoadSpec(specFile)
	if err != nil {
		return err
	}
	return core.RunSpec(spec, outputDir)
}
