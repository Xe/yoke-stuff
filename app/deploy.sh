#!/usr/bin/env bash

set -euo pipefail

yoke takeoff appairway oci://reg.xeiaso.net/x-app/airway:v1 -- --flight-url=oci://reg.xeiaso.net/x-app/flight:v1