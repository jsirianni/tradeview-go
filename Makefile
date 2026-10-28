GO ?= go
GOFMT ?= gofmt
TEST_ARGS ?=

.DEFAULT_GOAL := help

# Keep native Windows recipes independent of a Unix shell installation.
ifeq ($(OS),Windows_NT)
SHELL := cmd.exe
.SHELLFLAGS := /C
endif

.PHONY: help build test test-race coverage coverage-html fmt fmt-check vet generate generate-check deps tidy check ci clean

help:
	@echo make build - Build all Go packages
	@echo make test - Run tests, TEST_ARGS adds Go test flags
	@echo make test-race - Run tests with the race detector
	@echo make coverage - Write coverage.out and print coverage
	@echo make coverage-html - Also write coverage.html
	@echo make fmt - Format Go source files
	@echo make fmt-check - Check formatting without changing files
	@echo make vet - Run Go static analysis
	@echo make generate - Regenerate bindings from the pinned schema
	@echo make generate-check - Regenerate and check committed bindings
	@echo make deps - Download module dependencies
	@echo make tidy - Update go.mod and go.sum
	@echo make check - Check formatting, vet, and tests
	@echo make ci - Check generated bindings, build, and run all checks
	@echo make clean - Remove local build and coverage artifacts

build:
	$(GO) build ./...

test:
	$(GO) test $(TEST_ARGS) ./...

test-race:
	$(GO) test -race $(TEST_ARGS) ./...

coverage:
	$(GO) test -coverprofile=coverage.out $(TEST_ARGS) ./...
	$(GO) tool cover -func=coverage.out

coverage-html: coverage
	$(GO) tool cover -html=coverage.out -o coverage.html

fmt:
	$(GOFMT) -w .

fmt-check:
	$(eval format_files := $(shell $(GOFMT) -l .))
	$(if $(filter 0,$(.SHELLSTATUS)),,$(error gofmt failed with status $(.SHELLSTATUS)))
	$(if $(strip $(format_files)),$(error Run make fmt; unformatted files: $(format_files)))
	@echo Go formatting is clean

vet:
	$(GO) vet ./...

generate:
	$(GO) generate ./...

generate-check: generate
	git diff --exit-code -- api.gen.go api/operations.json
	git diff --cached --exit-code -- api.gen.go api/operations.json

deps:
	$(GO) mod download

tidy:
	$(GO) mod tidy

check: fmt-check vet test

# Finish generation before readers start, even when invoked with make -j.
ci: generate-check
	$(MAKE) check build test-race

clean:
	$(GO) clean ./...
ifeq ($(OS),Windows_NT)
	if exist coverage.out del /Q coverage.out
	if exist coverage.html del /Q coverage.html
else
	rm -f coverage.out coverage.html
endif
