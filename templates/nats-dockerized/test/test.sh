#!/bin/bash
# nats-dockerized test — quick node validation (used by deploy init --tested).
set -e
cd "$(dirname "$0")/.."
bash validate.sh
