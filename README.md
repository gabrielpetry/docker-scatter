# 🐳 Docker Scatter

[![Go Report Card](https://goreportcard.com/badge/github.com/gabrielpetry/docker-scatter)](https://goreportcard.com/report/github.com/gabrielpetry/docker-scatter)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gabrielpetry/docker-scatter)](https://github.com/gabrielpetry/docker-scatter)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Docker Scatter** is a high-performance Docker CLI plugin that orchestrates Docker Compose deployments across multiple nodes (contexts) concurrently. It allows you to treat a distributed cluster of Docker engines as a single, cohesive deployment target.

> [!TIP]
> This project follows the **Standard Go Project Layout** and is designed for production-readiness, extensibility, and observability.

---

## � Motivation

While **Kubernetes** is the gold standard for production orchestration, it often introduces unnecessary friction during local development. Managing a full cluster (or even `k3s`) just to test a multi-node setup can be cumbersome, especially when you want to share a simple `docker-compose` workflow with your team.

**Docker Scatter** bridges this gap. It maintains the simplicity of Docker Compose while allowing you to "scatter" your services across multiple machines. 

### The Use Case
Imagine you're developing data pipelines with **Apache Airflow**. Running Airflow alongside heavy containers like **Apache Spark**, **S3 (MinIO, SeaweedFS, RustFS ...)**, and multiple databases on a single machine can quickly exhaust your local resources. With Docker Scatter, you can:
*   Keep your **IDE and Airflow** running locally for a responsive development experience.
*   Offload **Spark, S3, and DBs** to a second machine or a home lab.

You get a distributed environment that "just works," giving your primary workstation room to breathe without the overhead of learning or maintaining a Kubernetes cluster for development.

---

## �🚀 Key Features

- **Multi-Context Orchestration**: Deploy your stack across multiple local or remote Docker contexts simultaneously.
- **Concurrent Execution**: Leverages Go goroutines to trigger deployments in parallel, drastically reducing boot times.
- **Dynamic Mesh Networking**: Automatically creates a secure WireGuard mesh using **Headscale** and **Tailscale** sidecars, enabling cross-node container communication with zero configuration.
- **Transparent Routing**: Integrates with **Traefik** to route traffic between contexts through the secure mesh.
- **Unified Observability**:
    - **Prefixed Logs**: Aggregates logs from all contexts with colored prefixes.
    - **Global PS**: A unified `ps` command showing status, health, CPU, and Memory usage across the entire cluster.
- **Native CLI Integration**: Acts as a first-class citizen in the Docker ecosystem (`docker scatter ...`).

---

## 🛠️ Installation

### 1. Build from Source
Ensure you have Go 1.25+ installed.

```bash
git clone https://github.com/gabrielpetry/docker-scatter.git
cd docker-scatter
make build
```

### 2. Install as a Plugin
Install the binary into the standard Docker CLI plugins directory:

```bash
make install
```

### 3. Verify
```bash
docker scatter --help
```

---

## ⚙️ Configuration

Create a `docker-scatter.yaml` in your project root:

```yaml
# Mesh networking (optional)
mesh:
  enable: true
  context: node-primary
  bind_address: 1.2.3.4
  bind_port: 8080

# Deployment targets
contexts:
  node-primary:
    env:
      ROLE: "database"
    profiles: ["db", "api"]
  node-secondary:
    env:
      ROLE: "worker"
    profiles: ["workers"]
```

---

## 📖 Usage

Docker Scatter proxies standard Compose commands to multiple contexts:

```bash
# Provision all nodes
docker scatter up -d

# Check cluster status (Unified view)
docker scatter ps --sort memory

# Tail logs from a specific service across all nodes
docker scatter logs -f api

# Direct Mesh access
docker scatter mesh node list
```

---

## 📐 Architecture & Standards

- **Standard Layout**: Separates the CLI entry point (`/cmd`) from core logic (`/internal`) to prevent import cycles and ensure clean boundaries.
- **Structured Logging**: Uses `log/slog` for modern, structured, and leveled logging.
- **Idiomatic Go**: Employs clean error wrapping, context-aware execution, and proper synchronization patterns.
- **Plugin Protocol**: Fully implements the Docker CLI Plugin Metadata protocol.

---

## 📂 Examples

Check out the `/examples` directory for pre-configured stacks:
- [**Basic**](./examples/basic): Simple multi-node deployment.
- [**Mesh**](./examples/mesh): Full-blown mesh networking with Traefik and cross-node communication.

---

## 📄 License
Distributed under the MIT License. See `LICENSE` for more information.
