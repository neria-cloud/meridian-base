# Meridian additions (patchset estimate-cost); included from the Makefile.
# cd through CURDIR: go resolves the working directory from $PWD, and a symlinked
# checkout path would otherwise not match the go.work location.
.PHONY: test-functional test-functional-pg
test-functional: ## Functional tests of the patchset APIs against the working tree (no Docker)
	cd $(CURDIR)/tests/functional && GOWORK=$(CURDIR)/tests/functional/go.work GOFLAGS=-mod=readonly go test -race -count=1 ./...
test-functional-pg: ## Same plus the PostgreSQL-backed tests (Docker)
	cd $(CURDIR)/tests/functional && GOWORK=$(CURDIR)/tests/functional/go.work GOFLAGS=-mod=readonly go test -race -count=1 -tags pg ./...
