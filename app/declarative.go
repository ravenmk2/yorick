package app

import (
	"github.com/sirupsen/logrus"
	"yorick/declarative"
)

func RunDeclarative(jobFile, outputDir string) error {
	logrus.Infof("Job file: %s", jobFile)

	spec, err := declarative.LoadSpec(jobFile)
	if err != nil {
		return err
	}
	return declarative.RunSpec(spec, outputDir)
}
