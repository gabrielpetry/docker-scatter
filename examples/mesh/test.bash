#!/usr/bin/env bash

set -euo pipefail

docker scatter up -d &&
 sleep 1

docker scatter mesh node list

curl -H 'Host: service-a.localhost' localhost:30080
curl -H 'Host: service-b.localhost' localhost:30080
curl -H 'Host: service-c.localhost' localhost:30080
curl -H 'Host: service-d.localhost' localhost:30080

docker scatter down --volumes
