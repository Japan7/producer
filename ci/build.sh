#!/bin/sh
out="$1"
export CGO_ENABLED=0

go build -o "${out}" -a -ldflags '-extldflags "-static"' .
