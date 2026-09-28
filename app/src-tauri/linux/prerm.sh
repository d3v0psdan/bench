#!/bin/sh
# Bench's package pre-remove script (deb prerm, rpm %preun), run as root.
#
# On removal (not an upgrade) it undoes the machine setup Bench made: the
# .test systemd-resolved rule and the local HTTPS authority in the system
# trust store. deb passes "remove" or "purge"; rpm passes the number of
# versions left, 0 on erase. Failures never block the removal.
case "$1" in
  remove | purge | 0) ;;
  *) exit 0 ;;
esac
helper=/usr/bin/bench-helper
if [ -x "$helper" ]; then
  "$helper" teardown -dns -installed-ca ||
    echo "bench: could not remove the HTTPS and DNS setup; run: sudo bench-helper teardown -dns -installed-ca" >&2
fi
exit 0
