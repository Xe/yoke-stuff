#!/usr/bin/env bash

set -euo pipefail

export GOOS=wasip1
export GOARCH=wasm
export AWS_PROFILE=tigris

go build -o x-app.wasm ./v1/flight
go build -o x-app-airway.wasm ./v1/airway

yoke stow ./x-app.wasm oci://registry.int.xeserv.us/x-app/flight:v1
yoke stow ./x-app-airway.wasm oci://registry.int.xeserv.us/x-app/airway:v1