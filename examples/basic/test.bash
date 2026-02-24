#!/usr/bin/env bash

set -euo pipefail

single_node() (
	# how a docker compose would actually be deployed
	docker compose --profile '*' up -d
	docker compose exec -it service-a-curl curl service-b
	docker compose --profile '*' ps
	docker compose --profile '*' down --volumes

)

scattered() (
	# Services are not in a mesh by default and this will fail
	# this is useful if your services bound to ports and you would be able to
	# communicate over the host ip
	docker scatter up -d
	docker scatter exec -it service-a-curl curl service-b
	docker scatter ps
	docker scatter down
)

"$@"
