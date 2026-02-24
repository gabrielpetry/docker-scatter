# Mesh Networking Example

This example demonstrates the advanced mesh networking capabilities of `docker-scatter`. It automatically sets up a WireGuard-based mesh network using **Headscale** and **Tailscale** sidecars, enabling secure, inter-container communication across different Docker contexts.

## How it works

1. **Headscale Deployment:** `docker-scatter` deploys a Headscale instance in one of your contexts (defined in `docker-scatter.yaml`).
2. **Transparent Overrides:** For every service in your `compose.yaml`, `docker-scatter` injects a Tailscale sidecar that joins the mesh.
3. **MagicDNS:** Services can reach each other using their service names as hostnames (e.g., `service-a` can curl `service-b`), regardless of which node they are running on.
4. **Traefik Integration:** If a Traefik service is detected, `docker-scatter` automatically configures it to route traffic to remote services through the mesh.

## Configuration

The `mesh` section in `docker-scatter.yaml` activates the feature:

```yaml
mesh:
  enable: true
  context: node-1
  bind_address: 192.168.1.10 # The IP of node-1 accessible by node-2
  bind_port: 8080
```

## Usage

### Prerequisites for RHEL/Fedora
If you are running on RHEL, Fedora, or other SELinux-enabled distributions, you might need to manually load several kernel modules required by Tailscale/WireGuard:

```bash
sudo modprobe ip_tables
sudo modprobe iptable_filter
sudo modprobe xt_mark
sudo modprobe iptable_nat
```

> [!NOTE]
> This example currently uses **Caddy** for service discovery. Other proxies like Traefik or Nginx may require manual resolution configuration or the use of Fully Qualified Domain Names (FQDNs) to work correctly with the Tailscale mesh.


1. **Start the mesh and services:**
   ```bash
   docker scatter up -d
   ```

2. **Verify connectivity:**
   Run the test script which curls services through a Caddy reverse proxy:
   ```bash
   bash test.bash
   ```

3. **Manage the mesh:**
   You can use the `mesh` subcommand to run headscale commands directly:
   ```bash
   docker scatter mesh node list
   ```

4. **Cleanup:**
   ```bash
   docker scatter down --volumes
   ```
