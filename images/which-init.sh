#!/bin/bash

# Check for systemd
if [ -x /bin/systemctl ] || [ -x /usr/bin/systemctl ]; then
    echo "systemd"
    exit 0
fi

# Check for OpenRC
if [ -x /sbin/openrc ] || [ -x /usr/sbin/rc ]; then
    echo "openrc"
    exit 0
fi

# If neither systemd nor OpenRC is found
exit 1