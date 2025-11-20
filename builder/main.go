package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

var (
	gitHubAPIToken                = os.Getenv("GH_API_TOKEN")
	overrideLastSuccessfulBuildSha = os.Getenv("OVERRIDE_LAST_SUCCESSFUL_BUILD_SHA")
	ignoredServices               = []string{} // Add a service to this array to ignore it
)

const (
	libsPath      = "libraries"
	servicesPath  = "services"
	packagePrefix = "github.com/hollow-cube/hc-services"
)

var (
	currentSha   = generateCurrentSha()
	githubAPIURL = generateGitHubAPIURL()
	NoRunsError  = errors.New("no successful runs found")
)

var buildAllFileTriggers = []string{
	"builder/main.go",
	".github/workflows/build.yaml",
}

// DependencyGraph is a map of modules to their dependency modules.
type DependencyGraph map[string][]string

func main() {
	modules := mustGetModules()
	lastSuccessfulBuildSha, err := getLastSuccessfulBuildSha()

	// If no previous runs or should build all, build all services
	if errors.Is(err, NoRunsError) {
		outputServices(filterForServices(modules))
		return
	}
	if err != nil {
		log.Fatalf("failed to get last successful build sha: %v", err)
	}

	changedFiles := mustGetChangedFiles(lastSuccessfulBuildSha)
	log.Printf("modules: %v\n", modules)

	if shouldBuildAll(changedFiles) {
		outputServices(filterForServices(modules))
		return
	}

	changedModules := getChangedModules(modules, changedFiles)
	log.Printf("changed modules: %v\n", changedModules)

	graph := mustBuildDependencyGraph(modules)
	servicesToBuild := determineServicesToBuild(changedModules, graph)
	outputServices(servicesToBuild)
}

func generateCurrentSha() string {
	if sha := os.Getenv("GITHUB_SHA"); sha != "" {
		return sha
	}

	output, err := runCommand("git", "rev-parse", "HEAD")
	if err != nil {
		log.Fatalf("failed to get current sha: %v", err)
	}

	return strings.TrimSpace(output)
}

func generateGitHubAPIURL() string {
	const baseURL = "https://api.github.com/repos/${{ github.repository }}/actions/workflows/${{ github.workflow }}/runs?branch=${{ github.ref_name }}&status=success&per_page=1"

	gitHubRef := os.Getenv("GITHUB_WORKFLOW_REF") // octocat/hello-world/.github/workflows/my-workflow.yml@refs/heads/my_branch
	log.Printf("workflow ref: %s\n", gitHubRef)

	// Extract workflow filename
	parts := strings.Split(gitHubRef, "/")
	for _, part := range parts {
		if strings.Contains(part, ".yaml") || strings.Contains(part, ".yml") {
			log.Printf("found short ref: %s\n", part)
			gitHubRef = part
			break
		}
	}

	// Remove ref suffix if present
	if strings.Contains(gitHubRef, "@") {
		gitHubRef = strings.Split(gitHubRef, "@")[0]
	}

	// Replace template variables
	url := baseURL
	url = strings.ReplaceAll(url, "${{ github.repository }}", os.Getenv("GITHUB_REPOSITORY"))
	url = strings.ReplaceAll(url, "${{ github.workflow }}", gitHubRef)
	url = strings.ReplaceAll(url, "${{ github.ref_name }}", os.Getenv("GITHUB_REF_NAME"))
	return url
}

func getLastSuccessfulBuildSha() (string, error) {
	if overrideLastSuccessfulBuildSha != "" {
		log.Printf("Using overridden last successful build sha: %s\n", overrideLastSuccessfulBuildSha)
		return overrideLastSuccessfulBuildSha, nil
	}

	req, err := http.NewRequest("GET", githubAPIURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Use GITHUB_TOKEN if available, otherwise fall back to GH_API_TOKEN
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = gitHubAPIToken
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get last successful build sha (URL: %s): %s", githubAPIURL, resp.Status)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	runs := data["workflow_runs"].([]interface{})
	if len(runs) == 0 {
		return "", NoRunsError
	}

	sha := runs[0].(map[string]interface{})["head_sha"].(string)
	return sha, nil
}

// getModules returns the list of modules referenced in the go work file.
// The return should be a list of modules in the format "services/mc-player-service", "libraries/libA", etc.
func getModules() ([]string, error) {
	output, err := runCommand("go", "list", "-m", "-f", "{{.Dir}}")
	if err != nil {
		return nil, fmt.Errorf("failed to get modules: %w", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	rawModules := strings.Split(output, "\n")
	modules := make([]string, 0, len(rawModules))

	for _, module := range rawModules {
		if module == "" || module == wd {
			continue
		}
		modules = append(modules, strings.TrimPrefix(module, wd+string(os.PathSeparator)))
	}

	return modules, nil
}

func getChangedFiles(lastSuccessfulBuildSha string) ([]string, error) {
	log.Printf("getting changed files between '%s' and '%s'\n", lastSuccessfulBuildSha, currentSha)
	output, err := runCommand("git", "diff", "--name-only", lastSuccessfulBuildSha, currentSha)
	if err != nil {
		return nil, fmt.Errorf("failed to get changed files: %w", err)
	}

	return strings.Split(output, "\n"), nil
}

func getChangedModules(modules []string, files []string) []string {
	changedModules := make([]string, 0, len(modules))
	for _, module := range modules {
		for _, file := range files {
			if strings.HasPrefix(file, module) {
				changedModules = append(changedModules, module)
				break
			}
		}
	}
	return changedModules
}

func getModuleDependencies(module string) ([]string, error) {
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Path}}", "all")
	cmd.Dir = module
	cmd.Env = append(os.Environ(), "GOWORK=off")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to get dependencies for module %s: %s: %w", module, stderr.String(), err)
	}

	lines := strings.Split(stdout.String(), "\n")
	dependencies := make([]string, 0)

	for _, line := range lines {
		dep := strings.TrimSpace(line)
		if !strings.HasPrefix(dep, packagePrefix) || dep == filepath.Join(packagePrefix, module) {
			continue
		}
		dependencies = append(dependencies, strings.TrimPrefix(dep, packagePrefix+"/"))
	}

	log.Printf("dependencies for module %s: %s\n", module, dependencies)
	return dependencies, nil
}

func buildDependencyGraph(modules []string) (DependencyGraph, error) {
	graph := make(DependencyGraph)
	for _, module := range modules {
		deps, err := getModuleDependencies(module)
		if err != nil {
			return nil, err
		}
		graph[module] = deps
	}

	log.Printf("dependency graph: %v\n", graph)
	return graph, nil
}

// shouldBuildAll returns true if any of the trigger files have changed.
// We rebuild all services if the builder or workflow file is changed.
func shouldBuildAll(changedFiles []string) bool {
	for _, file := range changedFiles {
		if slices.Contains(buildAllFileTriggers, file) {
			log.Printf("build all services as %s was changed\n", file)
			return true
		}
	}
	return false
}

func filterForServices(modules []string) []string {
	services := make([]string, 0)
	for _, module := range modules {
		if !strings.HasPrefix(module, servicesPath) {
			continue
		}

		serviceName := strings.TrimPrefix(module, servicesPath+"/")
		if !slices.Contains(ignoredServices, serviceName) {
			services = append(services, serviceName)
		}
	}
	return services
}

func determineServicesToBuild(changedModules []string, graph DependencyGraph) []string {
	servicesToBuild := make([]string, 0)

	// Add changed services
	for _, module := range changedModules {
		if !strings.HasPrefix(module, servicesPath) {
			continue
		}

		serviceName := strings.TrimPrefix(module, servicesPath+"/")
		if !slices.Contains(ignoredServices, serviceName) {
			servicesToBuild = append(servicesToBuild, serviceName)
		}
	}

	// Add services that depend on changed libraries
	for module, deps := range graph {
		moduleName := strings.TrimPrefix(module, servicesPath+"/")
		if slices.Contains(servicesToBuild, moduleName) {
			continue
		}

		for _, dep := range deps {
			if slices.Contains(changedModules, dep) {
				log.Printf("%s depends on %s, which has changed\n", module, dep)
				if !slices.Contains(ignoredServices, moduleName) {
					servicesToBuild = append(servicesToBuild, moduleName)
				}
				break
			}
		}
	}

	return servicesToBuild
}

// Helper functions

func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w", stderr.String(), err)
	}

	return stdout.String(), nil
}

func outputServices(services []string) {
	jsonOutput, err := json.Marshal(services)
	if err != nil {
		log.Fatalf("failed to marshal services to build: %v", err)
	}
	fmt.Println(string(jsonOutput))
}

func mustGetModules() []string {
	modules, err := getModules()
	if err != nil {
		log.Fatalf("failed to get modules: %v", err)
	}
	return modules
}

func mustGetChangedFiles(lastSuccessfulBuildSha string) []string {
	changedFiles, err := getChangedFiles(lastSuccessfulBuildSha)
	if err != nil {
		log.Fatalf("failed to get changed files: %v", err)
	}
	return changedFiles
}

func mustBuildDependencyGraph(modules []string) DependencyGraph {
	graph, err := buildDependencyGraph(modules)
	if err != nil {
		log.Fatalf("failed to build dependency graph: %v", err)
	}
	return graph
}