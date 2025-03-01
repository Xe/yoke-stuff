#!/usr/bin/env bash

set -euo pipefail

yoke takeoff appairway https://minio.xeserv.us/mi-static/yoke/x-app/airway/v1.wasm.gz -- --flight-url="https://minio.xeserv.us/mi-static/yoke/x-app/v1.wasm.gz?cachebuster=$(uuidgen)"