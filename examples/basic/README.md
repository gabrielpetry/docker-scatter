# Basic Example

This example demonstrates how to use `docker-scatter` to deploy a simple Nginx service across multiple Docker contexts.

## Prerequisites

- Two Docker contexts named `node-1` and `node-2`. 
  - If you don't have them, you can create dummy ones for testing:
    ```bash
    docker context create node-1 --docker "host=unix:///var/run/docker.sock"
    docker context create node-2 --docker "host=unix:///var/run/docker.sock"
    ```

## Configuration

The `docker-scatter.yaml` file defines which contexts to target:

```yaml
contexts:
  node-1:
    profiles: ["frontend"]
  node-2:
    profiles: ["frontend"]
```

## Usage

1. **Deploy the stack:**
   ```bash
   docker scatter up -d
   ```

2. **Check the status:**
   ```bash
   docker scatter ps
   ```

3. **View logs:**
   ```bash
   docker scatter logs -f nginx
   ```

4. **Tear down:**
   ```bash
   docker scatter down --volumes
   ```
