// Command ccwb-policy renders managed policies for the products selected on
// the command line, including Codex requirements when codex is selected, plus
// their shared AWS CLI profile. Run it from the repository root.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/ascending-llc/claude-3p-config/policy"
)

// output is one rendered file and where it goes, relative to the output directory.
type output struct {
	path string
	data []byte
}

const (
	defaultConfig = "config/config.json"
	exampleConfig = "config/config.example.json"
	defaultOut    = "dist"
	exampleOut    = "examples"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ccwb-policy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", defaultConfig, "deployment config to read")
	outDir := fs.String("out", defaultOut, "directory to write the macos/ and windows/ folders into")
	example := fs.Bool("example", false,
		"render "+exampleConfig+" with placeholders allowed into "+exampleOut+"/ (the committed examples)")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: ccwb-policy [-config path] [-out dir] claude|codex ...")
		_, _ = fmt.Fprintln(stderr, "       ccwb-policy -example [-config path] [-out dir]")

		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	products, err := parseProducts(fs.Args(), *example)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())

		fs.Usage()

		return 2
	}

	if *example {
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

		if !set["config"] {
			*configPath = exampleConfig
		}

		if !set["out"] {
			*outDir = exampleOut
		}
	}

	cfg, err := policy.Load(*configPath, policy.Options{Products: products, AllowPlaceholders: *example})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())

		return 1
	}

	artifacts, err := policy.Render(cfg)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())

		return 1
	}

	for _, o := range outputs(artifacts, *example) {
		path := filepath.Join(*outDir, o.path)
		if err := write(path, o.data); err != nil {
			_, _ = fmt.Fprintln(stderr, err.Error())

			return 1
		}

		_, _ = fmt.Fprintln(stdout, "wrote "+path)
	}

	return 0
}

func parseProducts(args []string, example bool) ([]policy.Product, error) {
	if example {
		if len(args) > 0 {
			return nil, errors.New("-example does not accept products")
		}

		return []policy.Product{policy.ProductClaude, policy.ProductCodex}, nil
	}

	if len(args) == 0 {
		return nil, errors.New("select at least one product: claude or codex")
	}

	var products []policy.Product
	for _, arg := range args {
		product := policy.Product(arg)
		if !product.Valid() {
			return nil, fmt.Errorf("unknown product %q", arg)
		}

		if !slices.Contains(products, product) {
			products = append(products, product)
		}
	}

	return products, nil
}

func outputs(a policy.Artifacts, example bool) []output {
	var out []output

	if example {
		out = []output{{"aws-config-CCWB.example.ini", a.AWSConfig}}

		if len(a.Mobileconfig) > 0 {
			out = append(out,
				output{"Claude-CCWB.example.mobileconfig", a.Mobileconfig},
				output{"Claude-CCWB.example.reg", a.Reg},
			)
		}

		if len(a.CodexRequirements) > 0 {
			out = append(out,
				output{"Codex-CCWB.example.mobileconfig", a.CodexMobileconfig},
				output{"codex-requirements-CCWB.example.toml", a.CodexRequirements},
			)
		}

		return out
	}

	out = []output{
		{filepath.Join("macos", "aws-config-CCWB.ini"), a.AWSConfig},
		{filepath.Join("windows", "aws-config-CCWB.ini"), a.AWSConfig},
	}

	if len(a.Mobileconfig) > 0 {
		out = append(out,
			output{filepath.Join("macos", "Claude-CCWB.mobileconfig"), a.Mobileconfig},
			output{filepath.Join("windows", "Claude-CCWB.reg"), a.Reg},
		)
	}

	if len(a.CodexRequirements) > 0 {
		out = append(out,
			output{filepath.Join("macos", "Codex-CCWB.mobileconfig"), a.CodexMobileconfig},
			output{filepath.Join("macos", "codex-requirements-CCWB.toml"), a.CodexRequirements},
			output{filepath.Join("windows", "codex-requirements-CCWB.toml"), a.CodexRequirements},
		)
	}

	return out
}

func write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating %s: %s", filepath.Dir(path), err.Error())
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %s", path, err.Error())
	}

	return nil
}
