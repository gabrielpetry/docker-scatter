package scatter

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/fatih/color"
)

func ExecuteDeployments(cfg *Config, composeArgs []string, sortField string) error {
	if len(composeArgs) > 0 && composeArgs[0] == "mesh" {
		return RunMeshCommand(cfg, composeArgs[1:])
	}

	isPsCmd := len(composeArgs) > 0 && composeArgs[0] == "ps"
	if isPsCmd {
		return ExecutePsCommand(cfg, sortField)
	}

	if cfg.Mesh.Enable && Contains(composeArgs, "up") {
		if err := InitializeMesh(cfg); err != nil {
			return fmt.Errorf("failed to initialize mesh: %w", err)
		}
	}

	// Filter contexts for 'exec' and 'logs' if a service is specified
	targetContexts := cfg.Contexts

	serviceName := ""
	if len(composeArgs) > 0 && (composeArgs[0] == "exec" || composeArgs[0] == "logs") {
		// Simplified service name detection: first non-flag argument after command
		for i := 1; i < len(composeArgs); i++ {
			if !strings.HasPrefix(composeArgs[i], "-") {
				// Potential service name
				// Avoid flags with values by being slightly smarter
				prev := composeArgs[i-1]
				if prev == "-u" || prev == "--user" || prev == "-w" || prev == "--workdir" || prev == "-e" || prev == "--env" || prev == "--index" {
					continue
				}
				serviceName = composeArgs[i]
				break
			}
		}

		if serviceName != "" {
			filtered := make(map[string]ContextConfig)
			var mu sync.Mutex
			var wg sync.WaitGroup

			for name, ctxCfg := range cfg.Contexts {
				wg.Add(1)
				go func(cName string, cCfg ContextConfig) {
					defer wg.Done()
					if serviceExistsInContext(cfg, cName, serviceName, cfg.Mesh.Enable) {
						mu.Lock()
						filtered[cName] = cCfg
						mu.Unlock()
					}
				}(name, ctxCfg)
			}
			wg.Wait()
			targetContexts = filtered
		}
	}

	if len(targetContexts) == 0 {
		if len(composeArgs) > 0 && (composeArgs[0] == "exec" || composeArgs[0] == "logs") {
			slog.Warn("No contexts found containing the specified service.", "service", serviceName)
			return nil
		}
		return fmt.Errorf("no contexts to target")
	}

	// If only one context is targeted, run it directly to support interactive TTY (-it)
	var finalErr error
	if len(targetContexts) == 1 {
		var name string
		var ctxCfg ContextConfig
		for n, c := range targetContexts {
			name = n
			ctxCfg = c
		}
		finalErr = RunDockerComposeInteractive(name, ctxCfg, composeArgs, cfg.Mesh.Enable, cfg)
	} else {
		var wg sync.WaitGroup
		errCh := make(chan error, len(targetContexts))

		for contextName, contextCfg := range targetContexts {
			wg.Add(1)
			go func(name string, ctxCfg ContextConfig) {
				defer wg.Done()
				if err := RunDockerCompose(name, ctxCfg, composeArgs, cfg.Mesh.Enable, cfg); err != nil {
					errCh <- fmt.Errorf("context %s failed: %w", name, err)
				}
			}(contextName, contextCfg)
		}

		wg.Wait()
		close(errCh)

		var errs []string
		for err := range errCh {
			errs = append(errs, err.Error())
		}
		if len(errs) > 0 {
			finalErr = fmt.Errorf("%s", strings.Join(errs, "; "))
		}
	}

	if cfg.Mesh.Enable && len(composeArgs) > 0 && composeArgs[0] == "down" {
		// Only cleanup mesh if we are doing a global down (no service names provided)
		isGlobalDown := true
		for i := 1; i < len(composeArgs); i++ {
			if !strings.HasPrefix(composeArgs[i], "-") {
				isGlobalDown = false
				break
			}
		}

		if isGlobalDown {
			CleanupMeshFiles(cfg, Contains(composeArgs, "--volumes") || Contains(composeArgs, "-v"))
		}
	}

	return finalErr
}

func serviceExistsInContext(cfg *Config, contextName string, serviceName string, meshEnabled bool) bool {
	args := getComposeArgs(contextName, cfg, meshEnabled)
	args = append(args, "config", "--services")

	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		return false
	}

	services := strings.Split(string(out), "\n")
	for _, s := range services {
		if strings.TrimSpace(s) == serviceName {
			return true
		}
	}
	return false
}

func RunDockerCompose(contextName string, ctxCfg ContextConfig, composeArgs []string, meshEnabled bool, globalCfg *Config) error {
	args := getComposeArgs(contextName, globalCfg, meshEnabled)
	args = append(args, composeArgs...)

	slog.Info("Running docker compose", "context", contextName, "command", "docker "+strings.Join(args, " "))
	cmd := exec.Command("docker", args...)

	// Inject environment variables specifically for this context
	env := os.Environ() // Start with base environment
	for k, v := range ctxCfg.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	// Setup logging prefixes
	stdoutPrefix := fmt.Sprintf("[%s] ", contextName)
	stderrPrefix := fmt.Sprintf("[%s] ", contextName)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	var wgLogs sync.WaitGroup
	wgLogs.Add(2)

	go func() {
		defer wgLogs.Done()
		streamOutput(stdoutPipe, os.Stdout, stdoutPrefix)
	}()

	go func() {
		defer wgLogs.Done()
		streamOutput(stderrPipe, os.Stderr, stderrPrefix)
	}()

	wgLogs.Wait()

	return cmd.Wait()
}

func RunDockerComposeInteractive(contextName string, ctxCfg ContextConfig, composeArgs []string, meshEnabled bool, globalCfg *Config) error {
	args := getComposeArgs(contextName, globalCfg, meshEnabled)
	args = append(args, composeArgs...)

	env := os.Environ()
	for k, v := range ctxCfg.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd := exec.Command("docker", args...)
	cmd.Env = env

	// Connect standard streams directly for interactive use
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func streamOutput(r io.Reader, w io.Writer, prefix string) {
	scanner := bufio.NewScanner(r)
	// Use a larger buffer for logs, as docker lines can be long
	const maxCapacity = 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		text := scanner.Text()

		// Colorize keywords
		text = strings.ReplaceAll(text, "Running", color.GreenString("Running"))
		text = strings.ReplaceAll(text, "Started", color.GreenString("Started"))
		text = strings.ReplaceAll(text, "Created", color.GreenString("Created"))
		text = strings.ReplaceAll(text, "Recreated", color.CyanString("Recreated"))
		text = strings.ReplaceAll(text, "Removed", color.RedString("Removed"))

		fmt.Fprintf(w, "%s%s\n", prefix, text)
	}
	if err := scanner.Err(); err != nil {
		slog.Error("Error reading output", "context", prefix, "error", err)
	}
}
