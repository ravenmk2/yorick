package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

const (
	AppName = "yorick"
)

func RunCliApp() error {
	app := NewCliApp()
	return app.Run(normalizeRunArgs(os.Args))
}

var runFlagsWithValue = map[string]bool{
	"-o": true, "--output": true,
	"-f": true, "--file": true,
	"--log-level": true,
}

// normalizeRunArgs hoists run flags ahead of the first positional argument:
// cli/v2 parses flags with the stdlib flag package, which stops at the
// first non-flag argument, so `run <file> -o out` would silently ignore -o.
func normalizeRunArgs(args []string) []string {
	if len(args) < 2 || args[1] != "run" {
		return args
	}
	flags := []string{}
	positionals := []string{}
	rest := args[2:]
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		name, _, hasValue := strings.Cut(arg, "=")
		switch {
		case arg == "--debug" || (hasValue && runFlagsWithValue[name]):
			flags = append(flags, arg)
		case runFlagsWithValue[arg]:
			flags = append(flags, arg)
			if i+1 < len(rest) {
				i++
				flags = append(flags, rest[i])
			}
		default:
			positionals = append(positionals, arg)
		}
	}
	normalized := []string{args[0], "run"}
	normalized = append(normalized, flags...)
	return append(normalized, positionals...)
}

func NewCliApp() *cli.App {
	app := &cli.App{
		Name:        AppName,
		Usage:       AppName,
		Description: "Yorick backup tool",
		Commands: []*cli.Command{
			NewRunCommand(),
		},
	}
	return app
}

func NewRunCommand() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Usage:     "Run a backup job",
		ArgsUsage: "<file>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "debug", Required: false, Value: false},
			&cli.StringFlag{Name: "log-level", Required: false},
			&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Required: false},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Required: false, Value: ".backup"},
		},
		Action: func(ctx *cli.Context) error {
			if ctx.IsSet("log-level") {
				level, err := logrus.ParseLevel(ctx.String("log-level"))
				if err != nil {
					return err
				}
				logrus.SetLevel(level)
			} else if ctx.Bool("debug") {
				logrus.SetLevel(logrus.DebugLevel)
			}
			jobFile, err := resolveJobFile(ctx)
			if err != nil {
				return err
			}
			outputDir := ctx.String("output")
			switch strings.ToLower(filepath.Ext(jobFile)) {
			case ".yaml", ".yml":
				return RunDeclarative(jobFile, outputDir)
			case ".js":
				return RunScripted(jobFile, outputDir)
			default:
				return fmt.Errorf("unsupported job file type: %s (expected .yaml, .yml or .js)", jobFile)
			}
		},
	}
}

// resolveJobFile picks the job file by precedence: positional
// argument > -f/--file > auto-detect.
func resolveJobFile(ctx *cli.Context) (string, error) {
	if ctx.Args().Len() > 0 {
		return ctx.Args().First(), nil
	}
	if ctx.IsSet("file") {
		return ctx.String("file"), nil
	}
	candidates := []string{".yorick.yaml", ".yorick.yml", ".yorick.js"}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no job file found (tried %s); pass a file argument or -f/--file", strings.Join(candidates, ", "))
}
