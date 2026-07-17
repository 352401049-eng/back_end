.PHONY: swagger run build

SWAG ?= $(shell go env GOPATH)/bin/swag

swagger:
	$(SWAG) init -g cmd/server/main.go -o docs --parseDependency --parseInternal

run:
	go run cmd/server/main.go

build:
	go build -o bin/server cmd/server/main.go
