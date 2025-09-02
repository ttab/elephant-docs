package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	elephantdocs "github.com/ttab/elephant-docs"
	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:   "elephant-docs",
		Action: generateAction,
		Flags: []cli.Flag{
			&cli.PathFlag{
				Name:  "config",
				Value: "elephant-docs.json",
			},
			&cli.PathFlag{
				Name:     "out",
				Usage:    "output directory for documentation",
				Required: true,
			},
			&cli.PathFlag{
				Name:  "base-path",
				Value: "",
			},
			&cli.StringFlag{
				Name:  "serve",
				Usage: "Serve documentation for local preview: -serve :8080",
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		TUIPrintln("error: %v", err)
		os.Exit(1)
	}
}

func generateAction(c *cli.Context) error {
	var (
		configPath = c.Path("config")
		outDir     = c.Path("out")
		basePath   = c.Path("base-path")
		serveAddr  = c.String("serve")
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

	err = elephantdocs.Generate(c.Context, outDir, basePath, conf, TUIPrintln)
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
