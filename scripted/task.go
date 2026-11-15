package scripted

import (
	"github.com/grafana/sobek"
	"github.com/sirupsen/logrus"
)

type TaskInfo struct {
	Name string
	dir  string
	fn   sobek.Callable
}

type TaskContext struct {
	logger *logrus.Logger
	info   *TaskInfo
}

func NewTaskContext(info *TaskInfo) *TaskContext {
	return &TaskContext{
		logger: logrus.StandardLogger(),
		info:   info,
	}
}
