package executor

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/gabrielpetry/docker-scatter/internal/config"
)

func ExecuteDeployments(cfg *config.Config, composeArgs []string, sortField string) error {
	isPsCmd := len(composeArgs) > 0 && composeArgs[0] == "ps"

	if isPsCmd {
		return executePsCommand(cfg, sortField)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(cfg.Contexts))

	for contextName, contextCfg := range cfg.Contexts {
		wg.Add(1)
		go func(name string, cfg config.ContextConfig) {
			defer wg.Done()
			if err := runDockerCompose(name, cfg, composeArgs); err != nil {
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
		return fmt.Errorf(strings.Join(errs, "; "))
	}

	return nil
}

func runDockerCompose(contextName string, cfg config.ContextConfig, composeArgs []string) error {
	// Construct command: docker --context <context_name> compose --profile <profile_1> ... <composeArgs>
	args := []string{"--context", contextName, "compose"}
	for _, profile := range cfg.Profiles {
		args = append(args, "--profile", profile)
	}
	args = append(args, composeArgs...)

	cmd := exec.Command("docker", args...)

	// Inject environment variables specifically for this context
	env := os.Environ() // Start with base environment
	for k, v := range cfg.Env {
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

func streamOutput(r io.Reader, w io.Writer, prefix string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fmt.Fprintf(w, "%s%s\n", prefix, scanner.Text())
	}
}
