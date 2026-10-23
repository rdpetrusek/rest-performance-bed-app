# ReST Bed dashboard. Needs Go and Node/npm.
.PHONY: all build ui run deploy wifi-watcher test test-go test-ui lint fmt vet clean

BIN := bin
STATIC := internal/web/static/app

# Recipes are sh (rm, cp, mkdir). Started from PowerShell or cmd, make finds no
# sh on PATH and falls back to cmd.exe, so put Git for Windows' usr/bin (sh and
# those tools) first, as Git Bash does.
ifeq ($(OS),Windows_NT)
export PATH := $(subst /mingw64/libexec/git-core,/usr/bin,$(shell git --exec-path));$(PATH)
SHELL := sh.exe
endif

all: build

# The Angular app, copied into the server's embedded static directory.
ui:
	cd ui && npx ng build
	rm -rf $(STATIC) && mkdir -p $(STATIC) && cp -r ui/dist/ui/browser/. $(STATIC)/

build: ui
	go build -o $(BIN)/ ./cmd/...

# Runs the server directly against BED_HOST (env, default 10.0.0.1), serving
# whatever is already in $(STATIC) (run `make ui` first for a real UI; without
# it this answers with the "not built" placeholder - fine when you're running
# `cd ui && npx ng serve` separately for UI work).
run:
	go run ./cmd/server

# Rebuilds the image and replaces the running container with it, against
# BED_HOST (env, default 10.0.0.1 - see compose.yaml). The named volume
# (restbed_data) is untouched, so the SQLite history survives the redeploy.
deploy:
	docker compose up -d --build --force-recreate

# Windows only for now (internal/wifi's netsh Radio). Needs HOME_WIFI_SSID
# and HOME_WIFI_PASSWORD set; WATCH_INTERVAL optionally overrides the 5m
# default sweep interval. Join/leaves the networks it scans - see AGENTS.md.
wifi-watcher:
	go run ./cmd/wifi-watcher

test: test-go test-ui

test-go:
	go test ./...

test-ui:
	cd ui && npx ng test --watch=false

fmt:
	gofmt -w cmd internal

vet:
	go vet ./...

lint: vet
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; echo "gofmt needed"; exit 1)
	cd ui && npx ng build --configuration development

clean:
	rm -rf $(BIN) ui/dist $(STATIC)
