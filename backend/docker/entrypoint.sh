#!/bin/sh
# Dispatches the single backend image to one of its commands. `exec` is what
# makes SIGTERM reach the Go process instead of this shell, so the API and the
# worker both stop gracefully.
set -eu

case "${1:-}" in
api | worker)
	exec "/app/$1"
	;;
*)
	exec "$@"
	;;
esac
