package scripted

import (
	"github.com/robertkrimen/otto"
	"github.com/sirupsen/logrus"
)

type TaskInfo struct {
	Name string
	dir  string
	fn   *otto.Value
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
