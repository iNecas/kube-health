NAME     := kube-health
PACKAGE  := github.com/inecas/$(NAME)
VERSION  := v0.1.0
GIT      := $(shell git rev-parse --short HEAD)
DATE     := $(shell date +%FT%T%Z)

default: help

build:     ## Builds the CLI
	go build \
	-ldflags "-w -X ${PACKAGE}/cmd.version=${VERSION} -X ${PACKAGE}/cmd.commit=${GIT} -X ${PACKAGE}/cmd.date=${DATE}" \
    -a -o bin/${NAME} ./main.go

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[38;5;69m%-30s\033[38;5;38m %s\033[0m\n", $$1, $$2}'
