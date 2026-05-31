#!/bin/sh
set -e

# Fix ownership of data volumes when running as root.
# Docker volumes are mounted as root:root by default, which prevents
# the gateway user from writing to /data/cache and /data/ipns.
if [ "$(id -u)" = "0" ]; then
    chown -R gateway:gateway /data

    echo "Dropping privileges to gateway user"
    exec su-exec gateway "$0" "$@"
fi

exec "$@"
