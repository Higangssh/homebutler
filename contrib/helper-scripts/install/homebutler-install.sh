#!/usr/bin/env bash

# Copyright (c) 2021-2026 community-scripts ORG
# Author: Sanghee Son (Higangssh)
# License: MIT | https://github.com/community-scripts/DevScripts/raw/main/LICENSE
# Source: https://github.com/Higangssh/homebutler

source /dev/stdin <<<"$FUNCTIONS_FILE_PATH"
color
verb_ip6
catch_errors
setting_up_container
network_check
update_os

fetch_and_deploy_gh_release "homebutler" "Higangssh/homebutler" "prebuild" "latest" "/opt/homebutler" "homebutler_*_linux_$(arch_resolve).tar.gz"

msg_info "Configuring homebutler"
mkdir -p /root/.config/homebutler
# The dashboard listens on 0.0.0.0; without a token its API answers anyone who
# can reach the container.
cat <<EOF >/root/.config/homebutler/config.yaml
web:
  token: $(openssl rand -hex 32)
EOF
chmod 600 /root/.config/homebutler/config.yaml
msg_ok "Configured homebutler"

msg_info "Creating Service"
cat <<EOF >/etc/systemd/system/homebutler.service
[Unit]
Description=homebutler dashboard
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/homebutler
ExecStart=/opt/homebutler/homebutler serve --config /root/.config/homebutler/config.yaml --host 0.0.0.0 --port 8080
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
systemctl enable -q --now homebutler
msg_ok "Created Service"

motd_ssh
customize
cleanup_lxc
