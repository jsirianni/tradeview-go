GO ?= go
GOFMT ?= gofmt
STATICCHECK_VERSION := v0.8.2
STATICCHECK_PACKAGES ?= ./...
GOSEC = $(GO) run github.com/securego/gosec/v2/cmd/gosec@v2.30.0
GOSEC_FLAGS = -exclude-generated -nosec-require-rules -nosec-require-justification -fmt=text
REVIVE = $(GO) run github.com/mgechev/revive@v1.17.1
ERRCHECK = $(GO) run github.com/kisielk/errcheck@v1.30.0
ERRCHECK_FLAGS = -blank -excludeonly -exclude scripts/errcheck-excludes.txt -ignoregenerated
GOIMPORTS = $(GO) run golang.org/x/tools/cmd/goimports@v0.52.0
GOVULNCHECK_VERSION := v1.9.0
TEST_ARGS ?=
FUZZ_TIME ?= 1000x
FUZZ_PARALLEL ?= 2

.DEFAULT_GOAL := help

.PHONY: help build test test-race fuzz coverage coverage-html coverage-check coverage-smoke fmt fmt-check fix fix-check fix-gate-test vet staticcheck govulncheck lint lint-smoke errcheck errcheck-smoke imports-check imports-smoke security security-smoke generate generate-check deps tidy check ci clean

help:
	@echo make build - Build all Go packages
	@echo make test - Run tests, TEST_ARGS adds Go test flags
	@echo make test-race - Run tests with the race detector
	@echo make fuzz - Run bounded parser and stream fuzz targets
	@echo make coverage - Write coverage.out and print coverage
	@echo make coverage-html - Also write coverage.html
	@echo make coverage-check - Run all tests with temporary grouped coverage and regression checks
	@echo make coverage-smoke - Verify coverage classification and regression failures
	@echo make fmt - Format Go source files
	@echo make fmt-check - Check formatting without changing files
	@echo make vet - Run Go static analysis
	@echo make staticcheck - Run pinned Staticcheck analysis
	@echo make govulncheck - Check reachable known vulnerabilities
	@echo make lint - Run pinned Revive rules on handwritten Go code
	@echo make lint-smoke - Prove configured Revive rules reject violations
	@echo make errcheck - Check ignored errors in handwritten Go code and tests
	@echo make errcheck-smoke - Prove unchecked errors fail beside accepted buffer writes
	@echo make imports-check - Check Go imports without changing files
	@echo make imports-smoke - Prove a missing import fails without changing the fixture
	@echo make security - Run pinned gosec on handwritten production packages
	@echo make security-smoke - Verify unsuppressed security findings fail
	@echo make generate - Regenerate bindings from the pinned schema
	@echo make generate-check - Regenerate and check committed bindings
	@echo make deps - Download module dependencies
	@echo make tidy - Update go.mod and go.sum
	@echo make check - Check formatting, vet, Staticcheck, and tests
	@echo make ci - Check generated bindings, build, and run all checks
	@echo make clean - Remove local build and coverage artifacts

build:
	$(GO) build ./...

test:
	$(GO) test $(TEST_ARGS) ./...

test-race:
	$(GO) test -race $(TEST_ARGS) ./...

fuzz:
	sh scripts/run-fuzz.sh "$(GO)" "$(FUZZ_TIME)" "$(FUZZ_PARALLEL)"

coverage:
	$(GO) test -coverprofile=coverage.out $(TEST_ARGS) ./...
	$(GO) tool cover -func=coverage.out

coverage-html: coverage
	$(GO) tool cover -html=coverage.out -o coverage.html

coverage-check:
	sh scripts/check-coverage.sh "$(GO)"

coverage-smoke:
	sh scripts/test-coverage.sh

fmt:
	$(GOFMT) -w .

fmt-check:
	$(eval format_files := $(shell $(GOFMT) -l .))
	$(if $(filter 0,$(.SHELLSTATUS)),,$(error gofmt failed with status $(.SHELLSTATUS)))
	$(if $(strip $(format_files)),$(error Run make fmt; unformatted files: $(format_files)))
	@echo Go formatting is clean
# Report the selected toolchain because modernization rules can change by patch.
fix:
	$(GO) version
	$(GO) fix ./...

fix-check: fix
	git diff --exit-code
	git diff --cached --exit-code

fix-gate-test:
	+sh scripts/test-fix-gate.sh "$(GO)" "$(MAKE)"

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) $(STATICCHECK_PACKAGES)

govulncheck:
	$(GO) version
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) -version
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

lint:
	$(REVIVE) -config revive.toml -formatter friendly ./...
	sh scripts/check-revive.sh $(REVIVE)

lint-smoke:
	sh scripts/check-revive.sh $(REVIVE)

errcheck:
	$(ERRCHECK) $(ERRCHECK_FLAGS) ./...

errcheck-smoke:
	sh scripts/check-errcheck.sh $(ERRCHECK)

imports-check:
	sh scripts/check-goimports.sh $(GOIMPORTS)

imports-smoke:
	sh scripts/test-goimports.sh $(GOIMPORTS)

security:
	$(GOSEC) $(GOSEC_FLAGS) ./...

security-smoke:
	sh scripts/check-gosec.sh $(GOSEC)

generate:
	$(GO) generate ./...

generate-check: generate
	git diff --exit-code -- pkg/tradeview/api.gen.go api/operations.json
	git diff --cached --exit-code -- pkg/tradeview/api.gen.go api/operations.json

deps:
	$(GO) mod download

tidy:
	$(GO) mod tidy

check: fmt-check vet staticcheck lint errcheck imports-check security security-smoke coverage-check

# Finish generation before readers start, even when invoked with make -j.
ci: generate-check
	$(MAKE) fix-check
	$(MAKE) check build test-race govulncheck fix-gate-test fuzz coverage-smoke errcheck-smoke imports-smoke

clean:
	$(GO) clean ./...
	rm -f coverage.out coverage.html
