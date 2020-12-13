.ONESHELL:
export SHELL := /usr/bin/bash
export SHELLOPTS := errexit:nounset:pipefail:xtrace

all: imports lint vet test

override fmts := $(patsubst %.go,build/goimports/%.fmt,$(shell find -path '*/.*' -prune -o -type f -name '*.go' -printf '%P\n'))
imports: force build/bin/goimports $(fmts)

build/bin/goimports:
	go build -o $@ golang.org/x/tools/cmd/goimports

build/goimports/%.fmt: %.go
	build/bin/goimports -format-only -w $(IMPORTSFLAGS) $<
	install -D /dev/null $@

lint: force build/bin/golint
	build/bin/golint -set_exit_status $(LINTFLAGS) ./...

build/bin/golint:
	go build -o $@ golang.org/x/lint/golint

vet: force
	go vet $(VETFLAGS) ./...

test: force
	go test $(TESTFLAGS) ./...

.PHONY: force
force:
