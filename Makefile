# ReST Bed dashboard. Needs Go and Node/npm.
.PHONY: all build ui run deploy wifi-watcher test test-go test-ui lint fmt vet clean

BIN := bin

# Recipes are sh (rm, cp, mkdir). Started from PowerShell or cmd, make finds no
# sh on PATH and falls back to cmd.exe, so put Git for Windows' usr/bin (sh and
# those tools) first, as Git Bash does.
ifeq ($(OS),Windows_NT)
export PATH := $(subst /mingw64/libexec/git-core,/usr/bin,$(shell git --exec-path));$(PATH)
SHELL := sh.exe
endif

all: build

# The Angular app's own commands live in ui/package.json; these only delegate.
ui:
	cd ui && npm run build

build:
	go build -o $(BIN)/ ./cmd/...

# Runs the API directly against BED_HOST (env, default 10.0.0.1). It serves
# only /api; for the UI run `cd ui && npm start` alongside it (its
# proxy.conf.json sends /api here).
run:
	go run ./cmd/server

# Rebuilds every service's image (see compose.yaml) and replaces the running
# containers, against BED_HOST (env, default 10.0.0.1). The named volume
# (restbed_data) is untouched, so the SQLite history survives the redeploy.
deploy:
	docker compose up -d --build --force-recreate --remove-orphans

# Windows only for now (internal/wifi's netsh Radio). Needs HOME_WIFI_SSID
# and HOME_WIFI_PASSWORD set; WATCH_INTERVAL optionally overrides the 5m
# default sweep interval. Join/leaves the networks it scans - see AGENTS.md.
wifi-watcher:
	go run ./cmd/wifi-watcher

test: test-go test-ui

test-go:
	go test ./...

test-ui:
	cd ui && npm run test:ci

fmt:
	gofmt -w cmd internal

vet:
	go vet ./...

lint: vet
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; echo "gofmt needed"; exit 1)
	cd ui && npm run lint

clean:
	rm -rf $(BIN)
	cd ui && npm run clean
