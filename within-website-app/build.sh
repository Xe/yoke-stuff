#!/usr/bin/env bash

set -euo pipefail

export GOOS=wasip1
export GOARCH=wasm
export AWS_PROFILE=tigris

go build -o x-app.wasm ./v1/flight
go build -o x-app-airway.wasm ./v1/airway

gzip -f *.wasm

aws s3 cp ./x-app.wasm.gz s3://xedn/yoke/x-app/v1.wasm.gz
aws s3 cp ./x-app-airway.wasm.gz s3://xedn/yoke/x-app/airway/v1.wasm.gz