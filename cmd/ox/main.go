package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hollow-cube/api-server/pkg/ox/gen"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "generate" {
		fmt.Fprintf(os.Stderr, "Usage: ox generate ./path/to/package.StructName\n")
		os.Exit(1)
	}

	target := os.Args[2]
	lastDot := strings.LastIndex(target, ".")
	if lastDot < 0 {
		fmt.Fprintf(os.Stderr, "Error: target must be in format ./path/to/package.StructName\n")
		os.Exit(1)
	}

	pkgPattern := target[:lastDot]
	structName := target[lastDot+1:]

	api, err := gen.Analyze(pkgPattern, structName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	specBytes, err := gen.GenerateOpenAPI(api)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating OpenAPI spec: %v\n", err)
		os.Exit(1)
	}

	codeBytes, err := gen.GenerateServer(api)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating server code: %v\n", err)
		os.Exit(1)
	}

	outDir := pkgPattern
	specPath := filepath.Join(outDir, "openapi.yaml")
	codePath := filepath.Join(outDir, "server.gen.go")

	if err := os.WriteFile(specPath, specBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", specPath, err)
		os.Exit(1)
	}
	if err := os.WriteFile(codePath, codeBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", codePath, err)
		os.Exit(1)
	}

	fmt.Printf("Generated %d endpoint(s):\n", len(api.Endpoints))
	for _, ep := range api.Endpoints {
		fmt.Printf("  %s %s -> %s\n", ep.Method, ep.Path, ep.Name)
	}
	fmt.Printf("Output:\n")
	fmt.Printf("  %s\n", specPath)
	fmt.Printf("  %s\n", codePath)
}
