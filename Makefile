GO ?= go
IMAGE ?= prueba-zapping:latest

.PHONY: fmt vet test run docker-build docker-save

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test -race -count=1 ./...

run:
	$(GO) run ./cmd/server

docker-build:
	docker build -t $(IMAGE) .

docker-save: docker-build
	mkdir -p dist
	docker save $(IMAGE) -o dist/prueba-zapping.tar
