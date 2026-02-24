package scatter

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// HandleCompletion processes dynamic completion logic for bash/zsh.
func HandleCompletion(args []string) {
	// args contains the current command line after 'docker scatter __complete'
	// The last element is the word being completed (may be empty)

	// If 'scatter' is still the first argument, skip it
	if len(args) > 0 && args[0] == "scatter" {
		args = args[1:]
	}

	if len(args) <= 1 {
		// Completing the first argument (command or global flag)
		fmt.Println("up")
		fmt.Println("down")
		fmt.Println("ps")
		fmt.Println("logs")
		fmt.Println("exec")
		fmt.Println("mesh")
		fmt.Println("-f")
		fmt.Println("--file")
		return
	}

	command := args[0]
	lastWord := args[len(args)-1]
	prevWord := ""
	if len(args) > 1 {
		prevWord = args[len(args)-2]
	}

	switch command {
	case "ps":
		if prevWord == "--sort" {
			fmt.Println("name")
			fmt.Println("status")
			fmt.Println("health")
			return
		}
		if !Contains(args, "--sort") {
			fmt.Println("--sort")
		}
		if !Contains(args, "-f") && !Contains(args, "--file") {
			fmt.Println("-f")
			fmt.Println("--file")
		}
	case "up", "down":
		if !Contains(args, "-f") && !Contains(args, "--file") {
			fmt.Println("-f")
			fmt.Println("--file")
		}
	case "logs":
		if prevWord == "-f" || prevWord == "--follow" {
			// Suggest services after -f/--follow if they exist
			printServices(args)
			return
		}
		if !Contains(args, "-f") && !Contains(args, "--follow") {
			fmt.Println("-f")
			fmt.Println("--follow")
		}
		if !Contains(args, "-f") && !Contains(args, "--file") {
			fmt.Println("-f")
			fmt.Println("--file")
		}
		// Always suggest services for logs
		printServices(args)
	case "exec":
		// Handle flags first
		if !Contains(args, "-i") && !Contains(args, "--interactive") {
			fmt.Println("-i")
			fmt.Println("--interactive")
		}
		if !Contains(args, "-t") && !Contains(args, "--tty") {
			fmt.Println("-t")
			fmt.Println("--tty")
		}
		if !Contains(args, "-u") && !Contains(args, "--user") {
			fmt.Println("-u")
			fmt.Println("--user")
		}
		if !Contains(args, "-w") && !Contains(args, "--workdir") {
			fmt.Println("-w")
			fmt.Println("--workdir")
		}
		if !Contains(args, "-e") && !Contains(args, "--env") {
			fmt.Println("-e")
			fmt.Println("--env")
		}
		if !Contains(args, "-d") && !Contains(args, "--detach") {
			fmt.Println("-d")
			fmt.Println("--detach")
		}
		if !Contains(args, "-T") && !Contains(args, "--no-TTY") {
			fmt.Println("-T")
			fmt.Println("--no-TTY")
		}
		if !Contains(args, "--privileged") {
			fmt.Println("--privileged")
		}
		if !Contains(args, "--index") {
			fmt.Println("--index")
		}
		// Suggest services
		printServices(args)
	}

	// Global flag completions if last word looks like a flag
	if lastWord == "--sort" || prevWord == "--sort" {
		fmt.Println("name")
		fmt.Println("status")
		fmt.Println("health")
	}
}

func printServices(args []string) {
	configFile := "docker-scatter.yaml"
	// Parse global flags at the beginning of args
	for i := 0; i < len(args); i++ {
		if (args[i] == "-f" || args[i] == "--file") && i+1 < len(args) {
			configFile = args[i+1]
			i++
		} else if strings.HasPrefix(args[i], "-") {
			// Skip other global flags if any
			continue
		} else {
			// Found the command, any -f after this belongs to the command
			break
		}
	}

	cfg, err := LoadConfig(configFile)
	if err != nil {
		return
	}

	services := make(map[string]struct{})
	var mu sync.Mutex
	var wg sync.WaitGroup

	for contextName, contextCfg := range cfg.Contexts {
		wg.Add(1)
		go func(name string, c ContextConfig) {
			defer wg.Done()
			cmdArgs := []string{"--context", name, "compose"}
			for _, p := range c.Profiles {
				cmdArgs = append(cmdArgs, "--profile", p)
			}
			cmdArgs = append(cmdArgs, "config", "--services")
			out, err := exec.Command("docker", cmdArgs...).Output()
			if err == nil {
				lines := strings.Split(string(out), "\n")
				mu.Lock()
				for _, line := range lines {
					s := strings.TrimSpace(line)
					if s != "" {
						services[s] = struct{}{}
					}
				}
				mu.Unlock()
			}
		}(contextName, contextCfg)
	}
	wg.Wait()

	for s := range services {
		fmt.Println(s)
	}
}

// HandleGenerateCompletion outputs the bash completion script.
func HandleGenerateCompletion(args []string) {
	fmt.Println(`_docker_scatter_completion() {
    local suggestions
    # Query the plugin for completions. Skip 'docker scatter' (the first two words)
    suggestions=$(docker scatter __complete "${COMP_WORDS[@]:2}")
    COMPREPLY=( $(compgen -W "$suggestions" -- "${COMP_WORDS[COMP_CWORD]}") )
}
# Register completion for both 'docker scatter' and 'docker-scatter'
complete -F _docker_scatter_completion docker-scatter
# Note: For 'docker scatter', the main docker completion usually handles plugins.
# if it doesn't, this bridge can be used:
# complete -F _docker_scatter_completion docker`)
}

func Contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
