#!/bin/sh

nerdctl compose -f ./../../customeros/deployment/docker-compose.yaml up -d postgres timescaledb timescaledb-sidecar otel-collector tempo nats grafana

