#!/usr/bin/env bash

set -euo pipefail

export AWS_PROFILE=tigris

AIRWAY_URL=$(aws s3 presign s3://xedn/yoke/x-app/airway/v1.wasm.gz)
FLIGHT_URL=$(aws s3 presign s3://xedn/yoke/x-app/v1.wasm.gz)

yoke takeoff appairway $AIRWAY_URL -- --flight-url="$FLIGHT_URL"