package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	elephantdocs "github.com/ttab/elephant-docs"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := cli.Command{
		Name:   "elephant-docs",
		Action: generateAction,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "config",
				Value:    "elephant-docs.json",
				TakesFile: true,
			},
			&cli.StringFlag{
				Name:      "out",
				Usage:     "output directory for documentation",
				Required:  true,
				TakesFile: true,
			},
			&cli.StringFlag{
				Name:      "base-path",
				Value:     "",
				TakesFile: true,
			},
			&cli.StringFlag{
				Name:  "serve",
				Usage: "Serve documentation for local preview: -serve :8080",
			},
			&cli.BoolFlag{
				Name:  "schema-prerelease",
				Usage: "Use the latest pre-release tag for schema documentation",
			},
		},
	}

	err := cmd.Run(context.Background(), os.Args)
	if err != nil {
		TUIPrintln("error: %v", err)
		os.Exit(1)
	}
}

func generateAction(ctx context.Context, cmd *cli.Command) error {
	var (
		configPath       = cmd.String("config")
		outDir           = cmd.String("out")
		basePath         = cmd.String("base-path")
		serveAddr        = cmd.String("serve")
		schemaPrerelease = cmd.Bool("schema-prerelease")
	)

	start := time.Now()

	err := os.RemoveAll(outDir)
	if err != nil {
		return fmt.Errorf("clear output directory: %w", err)
	}

	err = os.MkdirAll(outDir, 0o770)
	if err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	var conf elephantdocs.Config

	confData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	err = json.Unmarshal(confData, &conf)
	if err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}

	err = elephantdocs.Generate(ctx, outDir, basePath, conf, schemaPrerelease, TUIPrintln)
	if err != nil {
		return fmt.Errorf("generate documentation: %w", err)
	}

	duration := time.Since(start)

	TUIPrintln("Generated documentation in %s", duration.String())

	if serveAddr != "" {
		TUIPrintln("Serving docs at %s", serveAddr)

		err := http.ListenAndServe(serveAddr,
			http.FileServerFS(os.DirFS(outDir)))
		if err != nil {
			return fmt.Errorf("serve static files: %w", err)
		}
	}

	return nil
}

func TUIPrintln(format string, a ...any) {
	_, err := fmt.Fprintf(os.Stderr, format, a...)
	if err != nil {
		println(err.Error())

		return
	}

	println()
}
