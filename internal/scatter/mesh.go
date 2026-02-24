package scatter

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed headscale_template.yaml
var headscaleTemplate string

type ComposeStruct struct {
	Volumes  map[string]interface{} `yaml:"volumes,omitempty"`
	Services map[string]interface{} `yaml:"services,omitempty"`
}

func InitializeMesh(cfg *Config) error {
	if !cfg.Mesh.Enable {
		return nil
	}

	if err := os.MkdirAll(".docker-scatter", 0755); err != nil {
		return fmt.Errorf("failed to create .docker-scatter directory: %w", err)
	}

	ip := cfg.Mesh.BindAddress
	if ip == "" {
		slog.Info("Bind address not provided, discovering IP", "context", cfg.Mesh.Context)
		var err error
		ip, err = discoverHostIP(cfg.Mesh.Context)
		if err != nil {
			return fmt.Errorf("failed to discover host IP: %w", err)
		}
		slog.Info("Discovered IP", "ip", ip)
	}

	err := deployHeadscale(cfg, ip)
	if err != nil {
		return fmt.Errorf("failed to deploy headscale: %w", err)
	}

	authKey, err := generateHeadscaleAuthKey(cfg)
	if err != nil {
		return fmt.Errorf("failed to generate auth key: %w", err)
	}

	// First, gather all services and their labels across all contexts
	type ServiceConfig struct {
		Labels map[string]string `json:"labels"`
		Ports  []interface{}     `json:"ports"`
	}
	type ComposeConfig struct {
		Services map[string]ServiceConfig `json:"services"`
	}

	var allMeshMetadata []struct {
		Context string
		Service string
		Labels  map[string]string
		Ports   []interface{}
	}

	// Sort context names for stable iteration
	var ctxNames []string
	for name := range cfg.Contexts {
		ctxNames = append(ctxNames, name)
	}
	sort.Strings(ctxNames)

	for _, ctxName := range ctxNames {
		ctxConfig := cfg.Contexts[ctxName]
		args := []string{"--context", ctxName, "compose"}
		for _, profile := range ctxConfig.Profiles {
			args = append(args, "--profile", profile)
		}
		args = append(args, "config", "--format", "json")

		out, err := exec.Command("docker", args...).Output()
		if err != nil {
			slog.Warn("Failed to get config for context", "context", ctxName, "error", err)
			continue
		}

		var ccfg ComposeConfig
		if err := json.Unmarshal(out, &ccfg); err != nil {
			slog.Warn("Failed to parse config for context", "context", ctxName, "error", err)
			continue
		}

		for sName, sCfg := range ccfg.Services {
			allMeshMetadata = append(allMeshMetadata, struct {
				Context string
				Service string
				Labels  map[string]string
				Ports   []interface{}
			}{
				Context: ctxName,
				Service: sName,
				Labels:  sCfg.Labels,
				Ports:   sCfg.Ports,
			})
		}
	}

	// Prepare list of all service names for aliases
	var allMeshServices []string
	for _, m := range allMeshMetadata {
		allMeshServices = append(allMeshServices, m.Service)
	}

	headscaleURL := fmt.Sprintf("http://%s:%d", ip, cfg.Mesh.BindPort)
	for ctxName, ctxConfig := range cfg.Contexts {
		err := generateMeshOverrideForContext(ctxName, ctxConfig, cfg, headscaleURL, authKey, allMeshServices, allMeshMetadata)
		if err != nil {
			return fmt.Errorf("failed to generate override for context %s: %w", ctxName, err)
		}
	}

	return nil
}

func discoverHostIP(contextName string) (string, error) {
	cmd := exec.Command("docker", "--context", contextName, "run", "--rm", "--network", "host", "alpine", "sh", "-c", "ip route get 1 | sed -n 's/.*src \\([0-9.]*\\).*/\\1/p'")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "", fmt.Errorf("could not determine IP")
	}
	return ip, nil
}

func deployHeadscale(cfg *Config, ip string) error {
	var configContent string = headscaleTemplate
	if len(cfg.Mesh.Headscale) > 0 {
		data, err := yaml.Marshal(cfg.Mesh.Headscale)
		if err != nil {
			return fmt.Errorf("failed to marshal headscale config: %w", err)
		}
		configContent = string(data)
		// Perform replacements for placeholders
	}
	configContent = strings.ReplaceAll(configContent, "http://%s:%d", fmt.Sprintf("http://%s:%d", ip, cfg.Mesh.BindPort))
	configContent = strings.ReplaceAll(configContent, "0.0.0.0:%d", fmt.Sprintf("0.0.0.0:%d", cfg.Mesh.BindPort))
	// Also support %s for port if user used it
	configContent = strings.ReplaceAll(configContent, "http://%s:%s", fmt.Sprintf("http://%s:%d", ip, cfg.Mesh.BindPort))
	configContent = strings.ReplaceAll(configContent, "0.0.0.0:%s", fmt.Sprintf("0.0.0.0:%d", cfg.Mesh.BindPort))

	if err := os.WriteFile(".docker-scatter/headscale-config.yaml", []byte(configContent), 0644); err != nil {
		return err
	}

	hostname := cfg.Mesh.Hostname
	if hostname == "" {
		hostname = "headscale"
	}

	compose := ComposeStruct{
		Volumes: map[string]interface{}{
			"scatter_headscale_data": map[string]interface{}{},
		},
		Services: map[string]interface{}{
			"scatter-headscale": map[string]interface{}{
				"image":    "headscale/headscale:latest",
				"hostname": hostname,
				"restart":  "unless-stopped",
				"ports": []string{
					fmt.Sprintf("%d:%d", cfg.Mesh.BindPort, cfg.Mesh.BindPort),
				},
				"volumes": []string{
					"scatter_headscale_data:/var/lib/headscale",
					"./.docker-scatter/headscale-config.yaml:/etc/headscale/config.yaml:ro",
				},
				"command": "serve",
			},
		},
	}

	data, err := yaml.Marshal(compose)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf(".docker-scatter/compose-mesh-%s.yaml", cfg.Mesh.Context)
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return err
	}

	slog.Info("Deploying Headscale", "context", cfg.Mesh.Context)
	args := getComposeArgs(cfg.Mesh.Context, cfg, true)
	args = append(args, "--project-directory", ".", "up", "-d")
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func generateHeadscaleAuthKey(cfg *Config) (string, error) {
	authKeyFile := ".docker-scatter/authkey"
	if data, err := os.ReadFile(authKeyFile); err == nil {
		key := strings.TrimSpace(string(data))
		if key != "" {
			slog.Info("Reusing existing Headscale auth key")
			return key, nil
		}
	}

	contextName := cfg.Mesh.Context
	slog.Info("Generating Headscale pre-auth key")

	// Create user if not exists
	authArgs := getComposeArgs(contextName, cfg, true)
	authArgs = append(authArgs, "--project-directory", ".", "exec", "scatter-headscale", "headscale", "users", "create", "scatter")
	exec.Command("docker", authArgs...).Run()

	// Wait for headscale to be fully responsive
	time.Sleep(2 * time.Second)

	// Get user ID
	listArgs := getComposeArgs(contextName, cfg, true)
	listArgs = append(listArgs, "--project-directory", ".", "exec", "scatter-headscale", "headscale", "users", "list", "-o", "json")
	listCmd := exec.Command("docker", listArgs...)
	listOut, err := listCmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		return "", fmt.Errorf("failed to list users: %v - %s", err, stderr)
	}

	var users []struct {
		ID   uint   `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(listOut, &users); err != nil {
		return "", fmt.Errorf("failed to parse users JSON (%v): %s", err, string(listOut))
	}

	var userID uint
	for _, u := range users {
		if u.Name == "scatter" {
			userID = u.ID
			break
		}
	}

	if userID == 0 {
		return "", fmt.Errorf("user 'scatter' not found after creation")
	}

	keyArgs := getComposeArgs(contextName, cfg, true)
	keyArgs = append(keyArgs, "--project-directory", ".", "exec", "scatter-headscale", "headscale", "preauthkeys", "create", "--reusable", "-e", "24h", "-u", fmt.Sprintf("%d", userID))
	cmd := exec.Command("docker", keyArgs...)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		return "", fmt.Errorf("failed to create preauthkey: %v - %s", err, stderr)
	}

	key := strings.TrimSpace(string(out))
	// headscale outputs the key, might have other text, we need the last line or simple parse
	lines := strings.Split(key, "\n")
	key = strings.TrimSpace(lines[len(lines)-1])
	os.WriteFile(authKeyFile, []byte(key), 0600)
	return key, nil
}

func generateMeshOverrideForContext(contextName string, ctxConfig ContextConfig, cfg *Config, headscaleURL string, authKey string, allMeshServices []string, allMeshMetadata []struct {
	Context string
	Service string
	Labels  map[string]string
	Ports   []interface{}
}) error {
	// Find which services are in this context so we can exclude them from aliases
	args := []string{"--context", contextName, "compose"}
	for _, profile := range ctxConfig.Profiles {
		args = append(args, "--profile", profile)
	}
	args = append(args, "config", "--services")

	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		return fmt.Errorf("failed to get services for context %s: %v", contextName, err)
	}

	currentContextServicesStr := strings.TrimSpace(string(out))
	currentContextServices := make(map[string]bool)
	if currentContextServicesStr != "" {
		for _, s := range strings.Split(currentContextServicesStr, "\n") {
			currentContextServices[strings.TrimSpace(s)] = true
		}
	}

	override := ComposeStruct{
		Volumes:  map[string]interface{}{},
		Services: map[string]interface{}{},
	}

	if contextName == cfg.Mesh.Context {
		hostname := cfg.Mesh.Hostname
		if hostname == "" {
			hostname = "headscale"
		}

		override.Volumes["scatter_headscale_data"] = map[string]interface{}{}
		override.Services["scatter-headscale"] = map[string]interface{}{
			"image":    "headscale/headscale:latest",
			"hostname": hostname,
			"restart":  "unless-stopped",
			"ports": []string{
				fmt.Sprintf("%d:%d", cfg.Mesh.BindPort, cfg.Mesh.BindPort),
			},
			"volumes": []string{
				"scatter_headscale_data:/var/lib/headscale",
				"./.docker-scatter/headscale-config.yaml:/etc/headscale/config.yaml:ro",
			},
			"command": "serve",
		}
	}

	for service := range currentContextServices {
		if strings.Contains(service, "init") {
			continue
		}
		tsServiceName := fmt.Sprintf("%s-tailscale", service)
		tsVolumeName := fmt.Sprintf("ts_state_%s_%s", contextName, service)

		override.Volumes[tsVolumeName] = map[string]interface{}{}

		override.Services[tsServiceName] = map[string]interface{}{
			"image": "tailscale/tailscale:latest",
			"environment": []string{
				fmt.Sprintf("TS_AUTHKEY=%s", authKey),
				fmt.Sprintf("TS_EXTRA_ARGS=--login-server=%s --reset --accept-routes", headscaleURL),
				fmt.Sprintf("TS_HOSTNAME=%s", service),
				"TS_STATE_DIR=/var/lib/tailscale",
				"TS_USERSPACE=false",
				"TS_ACCEPT_DNS=true",
			},
			"volumes": []string{
				fmt.Sprintf("%s:/var/lib/tailscale", tsVolumeName),
				"/dev/net/tun:/dev/net/tun:z",
			},
			"cap_add": []string{
				"NET_ADMIN",
				"SYS_MODULE",
			},
			"network_mode": fmt.Sprintf("service:%s", service),
			"restart":      "unless-stopped",
		}

		// Removed empty service map to prevent unnecessary recreations by Docker Compose
	}

	// Check if traefik is in this context and inject labels from other contexts
	if _, ok := currentContextServices["traefik"]; ok {
		var remoteServices []string
		traefikLabels := make(map[string]string)
		for _, m := range allMeshMetadata {
			if m.Context == contextName {
				continue // Skip local labels, docker socket will handle them
			}

			remoteServices = append(remoteServices, m.Service)
			foundRouter := false
			for k, v := range m.Labels {
				if strings.HasPrefix(k, "traefik.http.routers") {
					if strings.HasSuffix(k, ".rule") {
						// Append the service name as a host rule to enable transparent mesh routing
						v = fmt.Sprintf("%s || Host(`%s`)", v, m.Service)
					}
					traefikLabels[k] = v
					foundRouter = true
				}
				if strings.HasPrefix(k, "traefik.http.middlewares") {
					traefikLabels[k] = v
				}
			}

			if foundRouter {
				// Inject the dynamic mesh-routing service URL for this remote service
				// Using FQDN to ensure resolution via MagicDNS
				traefikLabels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.url", m.Service)] = fmt.Sprintf("http://%s", m.Service)
			}
		}

		if len(traefikLabels) > 0 {
			traefikLabels["traefik.enable"] = "true"

			traefikSvc := override.Services["traefik"].(map[string]interface{})
			traefikSvc["labels"] = traefikLabels

			// Add network aliases for remote services to Traefik so clients can reach them transparently
			// if len(remoteServices) > 0 {
			// 	traefikSvc["networks"] = map[string]interface{}{
			// 		"default": map[string]interface{}{
			// 			"aliases": remoteServices,
			// 		},
			// 	}
			// }
		}
	}

	data, err := yaml.Marshal(override)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf(".docker-scatter/compose-mesh-%s.yaml", contextName)
	return os.WriteFile(filename, data, 0644)
}

func CleanupMeshFiles(cfg *Config, removeVolumes bool) {
	if !cfg.Mesh.Enable {
		return
	}

	slog.Info("Cleaning up Headscale", "context", cfg.Mesh.Context)
	args := getComposeArgs(cfg.Mesh.Context, cfg, true)
	args = append(args, "--project-directory", ".", "down")
	if removeVolumes {
		args = append(args, "-v")
	}
	exec.Command("docker", args...).Run()

	os.Remove(".docker-scatter/headscale-config.yaml")
	os.Remove(".docker-scatter/authkey")

	for ctxName := range cfg.Contexts {
		os.Remove(fmt.Sprintf(".docker-scatter/compose-mesh-%s.yaml", ctxName))
	}
}

func RunMeshCommand(cfg *Config, args []string) error {
	if !cfg.Mesh.Enable {
		return fmt.Errorf("mesh is not enabled in config")
	}

	headscaleSvc := "scatter-headscale"

	// Construct docker compose exec command
	cmdArgs := getComposeArgs(cfg.Mesh.Context, cfg, true)
	cmdArgs = append(cmdArgs, "--project-directory", ".", "exec", headscaleSvc, "headscale")
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command("docker", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

func getComposeArgs(contextName string, cfg *Config, meshEnabled bool) []string {
	args := []string{"--context", contextName, "compose"}

	// Find base compose file
	baseFiles := []string{"compose.yaml", "docker-compose.yaml", "docker-compose.yml"}
	for _, f := range baseFiles {
		if _, err := os.Stat(f); err == nil {
			args = append(args, "-f", f)
			break
		}
	}

	if meshEnabled {
		meshFile := fmt.Sprintf(".docker-scatter/compose-mesh-%s.yaml", contextName)
		if _, err := os.Stat(meshFile); err == nil {
			args = append(args, "-f", meshFile)
		}
	}

	if ctxCfg, ok := cfg.Contexts[contextName]; ok {
		for _, profile := range ctxCfg.Profiles {
			args = append(args, "--profile", profile)
		}
	}

	return args
}
