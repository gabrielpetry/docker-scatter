package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/gabrielpetry/docker-scatter/internal/config"
)

type ComposePsOutput struct {
	Name    string `json:"Name"`
	Project string `json:"Project"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Image   string `json:"Image"`
	Health  string `json:"Health"`
}

type DockerStatsOutput struct {
	Container string `json:"Container"`
	Name      string `json:"Name"`
	CPUPerc   string `json:"CPUPerc"`
	MemUsage  string `json:"MemUsage"`
}

type UnifiedPsRow struct {
	Context string
	Name    string
	Image   string
	State   string
	Health  string
	CPU     string
	Memory  string
}

func executePsCommand(cfg *config.Config, sortField string) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allRows []UnifiedPsRow
	errCh := make(chan error, len(cfg.Contexts))

	for contextName, contextCfg := range cfg.Contexts {
		wg.Add(1)
		go func(name string, cfg config.ContextConfig) {
			defer wg.Done()
			rows, err := fetchPsForContext(name, cfg)
			if err != nil {
				errCh <- fmt.Errorf("context %s ps failed: %w", name, err)
				return
			}
			mu.Lock()
			allRows = append(allRows, rows...)
			mu.Unlock()
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

	printPsTable(allRows, sortField)
	return nil
}

func fetchPsForContext(contextName string, cfg config.ContextConfig) ([]UnifiedPsRow, error) {
	// Construct command: docker --context <context_name> compose --profile <profile_1> ... ps --format json
	args := []string{"--context", contextName, "compose"}
	for _, profile := range cfg.Profiles {
		args = append(args, "--profile", profile)
	}
	args = append(args, "ps", "--format", "json")

	cmd := exec.Command("docker", args...)

	env := os.Environ()
	for k, v := range cfg.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var containers []ComposePsOutput
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var c ComposePsOutput
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, err
		}
		containers = append(containers, c)
	}

	if len(containers) == 0 {
		return nil, nil
	}

	// Fetch stats for these containers
	var containerNames []string
	for _, c := range containers {
		containerNames = append(containerNames, c.Name)
	}

	statsArgs := []string{"--context", contextName, "stats", "--no-stream", "--format", "json"}
	statsArgs = append(statsArgs, containerNames...)

	statsCmd := exec.Command("docker", statsArgs...)
	statsOut, err := statsCmd.Output()
	if err != nil {
		// Tolerate stats error occasionally, container might have exited
		fmt.Fprintf(os.Stderr, "[%s|WARN] could not fetch stats: %v\n", contextName, err)
	}

	statsMap := make(map[string]DockerStatsOutput)
	if err == nil {
		statsLines := strings.Split(strings.TrimSpace(string(statsOut)), "\n")
		for _, line := range statsLines {
			if line == "" {
				continue
			}
			var s DockerStatsOutput
			if err := json.Unmarshal([]byte(line), &s); err == nil {
				statsMap[s.Name] = s
			}
		}
	}

	var unified []UnifiedPsRow
	for _, c := range containers {
		cpu := "-"
		mem := "-"
		if stat, ok := statsMap[c.Name]; ok {
			cpu = stat.CPUPerc
			mem = stat.MemUsage
		}
		
		health := c.Health
		if health == "" {
			health = "-"
		}

		unified = append(unified, UnifiedPsRow{
			Context: contextName,
			Name:    c.Name,
			Image:   c.Image,
			State:   c.State,
			Health:  health,
			CPU:     cpu,
			Memory:  mem,
		})
	}

	return unified, nil
}

// parsing util for sorting
func parseCPU(cpu string) float64 {
	var val float64
	fmt.Sscanf(cpu, "%f%%", &val)
	return val
}

func parseMem(mem string) float64 {
	// "1.36GiB / 15.29GiB" -> want the 1.36GiB part
	parts := strings.Split(mem, "/")
	if len(parts) == 0 {
		return 0
	}
	raw := strings.TrimSpace(parts[0])

	var val float64
	var unit string
	
	// Handles matching numbers and units correctly. Simplified.
	for i, c := range raw {
		if (c < '0' || c > '9') && c != '.' {
			unit = raw[i:]
			fmt.Sscanf(raw[:i], "%f", &val)
			break
		}
	}

	multiplier := 1.0
	switch strings.ToUpper(unit) {
	case "KIB", "KB":
		multiplier = 1024
	case "MIB", "MB":
		multiplier = 1024 * 1024
	case "GIB", "GB":
		multiplier = 1024 * 1024 * 1024
	case "TIB", "TB":
		multiplier = 1024 * 1024 * 1024 * 1024
	case "B":
		multiplier = 1
	}

	return val * multiplier
}

func printPsTable(rows []UnifiedPsRow, sortField string) {
	if len(rows) == 0 {
		fmt.Println("No containers running.")
		return
	}

	if sortField == "cpu" {
		sort.Slice(rows, func(i, j int) bool {
			return parseCPU(rows[i].CPU) > parseCPU(rows[j].CPU)
		})
	} else if sortField == "memory" || sortField == "mem" {
		sort.Slice(rows, func(i, j int) bool {
			return parseMem(rows[i].Memory) > parseMem(rows[j].Memory)
		})
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "CONTEXT\tNAME\tIMAGE\tSTATUS\tHEALTH\tCPU %\tMEM USAGE / LIMIT")
	
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Context, r.Name, r.Image, r.State, r.Health, r.CPU, r.Memory,
		)
	}
	w.Flush()
}
