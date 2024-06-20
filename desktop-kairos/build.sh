#!/bin/bash

set -e

export SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
export IMAGE=kairos-desktop

echo "Building the image"
docker build -t "$IMAGE" -f "$SCRIPT_DIR/Dockerfile" "$SCRIPT_DIR"

echo "Writing the config.yaml file"
cat << EOF > $SCRIPT_DIR/config.yaml
#cloud-config
users:
  - name: kairos
    passwd: kairos

install:
   auto: true
   device: "auto"
   reboot: true

k3s:
  enabled: true

write_files:
- path: /etc/pam.d/cage
  content: |
    auth           required        pam_unix.so nullok
    account        required        pam_unix.so
    session        required        pam_unix.so
    session        required        pam_systemd.so

- path: /etc/systemd/system/cage@.service
  persmissions: "0644"
  owner: "root"
  content: |
    # This is a system unit for launching Cage with auto-login as the
    # user configured here. For this to work, wlroots must be built
    # with systemd logind support.

    [Unit]
    Description=Cage Wayland compositor on %I
    # Make sure we are started after logins are permitted. If Plymouth is
    # used, we want to start when it is on its way out.
    After=systemd-user-sessions.service plymouth-quit-wait.service
    # Since we are part of the graphical session, make sure we are started
    # before it is complete.
    Before=graphical.target
    # On systems without virtual consoles, do not start.
    ConditionPathExists=/dev/tty0
    # D-Bus is necessary for contacting logind, which is required.
    Wants=dbus.socket systemd-logind.service
    After=dbus.socket systemd-logind.service
    # Replace any (a)getty that may have spawned, since we log in
    # automatically.
    Conflicts=getty@%i.service
    After=getty@%i.service

    [Service]
    Type=simple
    ExecStart=/usr/bin/cage thunar
    Restart=always
    # TODO: changing to "root" here makes it work. Hmm
    User=kairos
    # Log this user with utmp, letting it show up with commands 'w' and
    # 'who'. This is needed since we replace (a)getty.
    UtmpIdentifier=%I
    UtmpMode=user
    # A virtual terminal is needed.
    TTYPath=/dev/%I
    TTYReset=yes
    TTYVHangup=yes
    TTYVTDisallocate=yes
    # Fail to start if not controlling the virtual terminal.
    StandardInput=tty-fail

    # Set up a full (custom) user session for the user, required by Cage.
    PAMName=cage

    [Install]
    WantedBy=graphical.target
    Alias=display-manager.service
    DefaultInstance=tty7

# https://github.com/cage-kiosk/cage/wiki/Starting-Cage-on-boot-with-systemd
runcmd:
  - systemctl enable cage@tty1.service
  - systemctl set-default graphical.target
EOF

echo "Building the ISO"
docker run -v "$SCRIPT_DIR"/config.yaml:/config.yaml \
             -v "$SCRIPT_DIR"/build:/tmp/auroraboot \
             -v /var/run/docker.sock:/var/run/docker.sock \
             --rm -ti quay.io/kairos/auroraboot \
             --set container_image="docker://$IMAGE" \
             --set "disable_http_server=true" \
             --set "disable_netboot=true" \
             --cloud-config /config.yaml \
             --set "state_dir=/tmp/auroraboot"

docker run -v "$SCRIPT_DIR"/build:/tmp/build $IMAGE chown -R 1000:1001 /tmp/build
