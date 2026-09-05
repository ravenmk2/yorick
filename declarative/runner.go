package declarative

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

func RunSpec(spec *Spec, outputDir string) error {
	logger := logrus.StandardLogger()
	if spec.Name != "" {
		logger.Infof("Spec: %s", spec.Name)
	}
	logger.Infof("Output directory: %s", outputDir)

	envMap := buildEnvMap()
	count := len(spec.Tasks)
	for i, task := range spec.Tasks {
		logger.Infof("[%d/%d] Task: %s", i+1, count, task.Name)

		scope := NewExprScope(spec.Vars, envMap)
		if task.If != "" {
			ok, err := spec.programs.evalBool(task.If, scope)
			if err != nil {
				return fmt.Errorf("task %q if: %w", task.Name, err)
			}
			if !ok {
				logger.Info("Skipped (condition not met)")
				continue
			}
		}

		destDir := filepath.Join(outputDir, task.Dest)
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return err
		}
		logger.Infof("Destination: %s", destDir)

		ctx := &StepContext{logger: logger, destDir: destDir}
		for j, step := range task.Steps {
			if err := runStep(spec, ctx, scope, task, step, j); err != nil {
				return err
			}
		}
	}

	logger.Info("All done")
	return nil
}

func runStep(spec *Spec, ctx *StepContext, scope *ExprScope, task *TaskSpec, step *StepSpec, index int) error {
	where := stepWhere(task, index, step)

	if step.If != "" {
		ok, err := spec.programs.evalBool(step.If, scope)
		if err != nil {
			return fmt.Errorf("%s if: %w", where, err)
		}
		if !ok {
			ctx.logger.Infof("Step %d skipped (condition not met)", index+1)
			return nil
		}
	}

	output, err := dispatchStep(spec, ctx, scope, step, where)
	if err != nil {
		return err
	}
	registerStepOutput(scope, step, output)
	return nil
}

func dispatchStep(spec *Spec, ctx *StepContext, scope *ExprScope, step *StepSpec, where string) (any, error) {
	args, err := spec.programs.interpolate(step.Args, scope)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", where, err)
	}
	output, err := stepFuncs[step.Func].run(ctx, args.(map[string]any))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", where, err)
	}
	return output, nil
}

// registerStepOutput publishes steps.<id>.output; steps without an id are
// not registered.
func registerStepOutput(scope *ExprScope, step *StepSpec, output any) {
	if step.Id == "" {
		return
	}
	scope.Steps[step.Id] = map[string]any{"output": output}
}
