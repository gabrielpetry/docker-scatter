.PHONY: build
build:
	go build -o docker-scatter ./cmd/docker-scatter

.PHONY: install
install:
	mkdir -p ~/.docker/cli-plugins/
	cp docker-scatter ~/.docker/cli-plugins/

.PHONY: compose
compose:
	go run ./cmd/docker-scatter