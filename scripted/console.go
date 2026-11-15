package scripted

import (
	"strings"

	"github.com/grafana/sobek"
	"github.com/sirupsen/logrus"
)

func (o *FunctionsObject) registerConsole() error {
	write := func(level logrus.Level) func(sobek.FunctionCall) sobek.Value {
		return func(call sobek.FunctionCall) sobek.Value {
			parts := make([]string, len(call.Arguments))
			for i, arg := range call.Arguments {
				parts[i] = arg.String()
			}
			o.logger.Log(level, strings.Join(parts, " "))
			return sobek.Undefined()
		}
	}
	noop := func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() }
	methods := map[string]any{
		"log":   write(logrus.InfoLevel),
		"info":  write(logrus.InfoLevel),
		"debug": write(logrus.DebugLevel),
		"warn":  write(logrus.WarnLevel),
		"error": write(logrus.ErrorLevel),
		// These methods were also no-ops in otto.
		"dir":     noop,
		"time":    noop,
		"timeEnd": noop,
		"trace":   noop,
		"assert":  noop,
	}
	console := o.vm.NewObject()
	for name, fn := range methods {
		if err := console.Set(name, fn); err != nil {
			return err
		}
	}
	return o.vm.Set("console", console)
}
