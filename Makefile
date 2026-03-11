.PHONY: generate build test test-race up down image lint clean

BINARY := stenosaur
CMD     := ./cmd/stenosaur

## generate: run templ codegen and Tailwind CSS build
generate:
	templ generate ./...
	tailwindcss -i ./pkg/controller/ui/static/tw.src.css \
	            -o ./pkg/controller/ui/static/tw.css \
	            --content './pkg/controller/ui/**/*.templ' \
	            --minify

## build: compile the binary
build: generate
	go build -o bin/$(BINARY) $(CMD)

## test: run all tests
test:
	go test ./...

## test-race: run all tests with race detector
test-race:
	go test -race ./...

## lint: run vet and staticcheck
lint:
	go vet ./...

## up: build image and start with Docker Compose
up:
	docker compose up --build

## down: stop Docker Compose services
down:
	docker compose down

## image: build the Docker image only
image:
	docker compose build

## clean: remove build artifacts
clean:
	rm -rf bin/

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/## //'
