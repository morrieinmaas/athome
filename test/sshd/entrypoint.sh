#!/bin/sh
set -eu
: "${AUTHORIZED_KEY:?set AUTHORIZED_KEY to the first public key}"
mkdir -p /home/dev/.ssh
printf '%s\n' "$AUTHORIZED_KEY" > /home/dev/.ssh/authorized_keys
chown -R dev:dev /home/dev/.ssh
chmod 700 /home/dev/.ssh
chmod 600 /home/dev/.ssh/authorized_keys
exec /usr/sbin/sshd -D -e
