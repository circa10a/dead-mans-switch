# Tailscale + MagicDNS HTTPS Integration Guide

Dead Man's Switch supports custom TLS certificates, which makes it a natural fit for [Tailscale's HTTPS support](https://tailscale.com/kb/1153/enabling-https). This guide walks you through serving Dead Man's Switch over HTTPS on your tailnet using MagicDNS with zero port-forwarding and no public exposure.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Setup](#setup)
  - [1. Enable MagicDNS and HTTPS](#1-enable-magicdns-and-https)
  - [2. Generate a TLS Certificate](#2-generate-a-tls-certificate)
  - [3. Start the Container](#3-start-the-container)
- [Combining with Authentication](#combining-with-authentication)
- [Certificate Renewal](#certificate-renewal)
- [Subnet Routing (docker)](#subnet-routing-docker)
- [Troubleshooting](#troubleshooting)
- [References](#references)

## Overview

[Tailscale](https://tailscale.com/) creates a private WireGuard mesh network (a "tailnet") between your devices. With [MagicDNS](https://tailscale.com/kb/1081/magicdns) enabled, each device gets a hostname like `my-server.tail-name.ts.net`. Tailscale can provision trusted TLS certificates for these hostnames via Let's Encrypt, meaning you get valid HTTPS without exposing anything to the public internet.

Dead Man's Switch accepts custom TLS cert/key files, so you can point it directly at the certs Tailscale generates and bind-mount them into the container.

### Why Use This

- **Private by default** — Only devices on your tailnet can reach the service. No port-forwarding, no firewall rules.
- **Valid HTTPS** — Browser-trusted TLS certificates (required for push notifications) without needing a public domain.
- **Simple** — No reverse proxy, no CertMagic/Let's Encrypt config, no DNS challenge setup.

## Prerequisites

- Docker and Docker Compose
- [Tailscale](https://tailscale.com/download) installed and running on the Docker host
- MagicDNS and HTTPS enabled in the [Tailscale admin console](https://login.tailscale.com/admin/dns)

## Setup

### 1. Enable MagicDNS and HTTPS

1. Open the [Tailscale admin console](https://login.tailscale.com/admin/dns)
2. Under **DNS**, ensure **MagicDNS** is enabled
3. Under **HTTPS Certificates**, toggle **Enable HTTPS**

Find your machine's MagicDNS hostname:

```bash
tailscale status
```

It will look like `my-server.tail-name.ts.net`.

### 2. Generate a TLS Certificate

Run `tailscale cert` on the Docker host to create a Let's Encrypt certificate for your MagicDNS hostname:

```bash
sudo mkdir -p /etc/dead-mans-switch
sudo tailscale cert \
  --cert-file /etc/dead-mans-switch/tls.crt \
  --key-file /etc/dead-mans-switch/tls.key \
  my-server.tail-name.ts.net
```

Replace `my-server.tail-name.ts.net` with your actual hostname.

### 3. Start the Container

Create a `docker-compose.yaml`:

```yaml
services:
  dead-mans-switch:
    image: circa10a/dead-mans-switch:latest
    restart: unless-stopped
    ports:
      - "8443:8443"
    environment:
      DEAD_MANS_SWITCH_PORT: "8443"
      DEAD_MANS_SWITCH_TLS_CERTIFICATE: /certs/tls.crt
      DEAD_MANS_SWITCH_TLS_KEY: /certs/tls.key
    volumes:
      - /etc/dead-mans-switch:/certs:ro
      - dms-data:/data

volumes:
  dms-data:
```

```bash
docker compose up -d
```

Visit `https://my-server.tail-name.ts.net:8443` from any device on your tailnet. Your browser will show a valid, trusted certificate.

> [!CAUTION]
> Do **not** use `--auto-tls` alongside `--tls-certificate`/`--tls-key`. The `--auto-tls` flag is for CertMagic's built-in ACME flow and conflicts with custom certificates.

## Certificate Renewal

Tailscale certificates are issued by Let's Encrypt and are valid for 90 days. `tailscale cert` is a no-op when the existing certificate is still valid, so it's safe to run on a schedule.

Add a cron job on the Docker host to renew the cert and restart the container:

```bash
# Renew daily at 3 AM
0 3 * * * tailscale cert \
  --cert-file /etc/dead-mans-switch/tls.crt \
  --key-file /etc/dead-mans-switch/tls.key \
  my-server.tail-name.ts.net \
  && docker compose -f /path/to/docker-compose.yaml restart dead-mans-switch
```

## Subnet Routing (docker)

[Link to official docs](https://tailscale.com/docs/features/subnet-routers)

If you're running Tailscale in a Docker container and need to access other devices on your LAN (not just the Docker host), you'll need to configure subnet routing. This is useful when you want to reach services like a NAS, DNS server, or other devices on your home network from your phone or laptop while away from home.

I use this to keep the app off of the internet + HTTPs via MagicDNS/HTTPS enabled for push notifications to work.

### Prerequisites

- Tailscale container running with `network_mode: host`
- `--advertise-routes=192.168.1.0/24` (or your LAN CIDR) in `TS_EXTRA_ARGS`

### 1. Enable IP Forwarding

The Linux kernel must forward packets between the Tailscale and LAN interfaces:

```bash
sudo sysctl -w net.ipv4.ip_forward=1
echo 'net.ipv4.ip_forward = 1' | sudo tee /etc/sysctl.d/99-tailscale.conf
```

### 2. Configure iptables

Docker sets the default `FORWARD` policy to `DROP`, which blocks subnet-routed traffic. Add rules to allow forwarding and masquerade the source IP so LAN devices can send responses back:

```bash
# Allow traffic from Tailscale to LAN
sudo iptables -A FORWARD -i tailscale0 -o eth0 -j ACCEPT

# Allow return traffic from LAN to Tailscale
sudo iptables -A FORWARD -i eth0 -o tailscale0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT

# Masquerade so LAN devices see traffic from the host's IP
sudo iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
```

> [!NOTE]
> Replace `eth0` with your LAN interface name if different (e.g., `wlan0`, `end0`).

To persist across reboots:

```bash
sudo mkdir -p /etc/iptables
sudo iptables-save > /etc/iptables/rules.v4
```

### 3. Approve the Subnet Route

1. Open the [Tailscale admin console](https://login.tailscale.com/admin/machines)
2. Click on your machine
3. Under **Subnet routes**, approve `192.168.1.0/24`

### 4. Configure DNS (Optional)

If you run a local DNS server (e.g., AdGuard Home, Pi-hole), you can use it over Tailscale:

1. In the [Tailscale admin console](https://login.tailscale.com/admin/dns), go to **Nameservers**
2. Add your DNS server's LAN IP as a **Global nameserver**
3. Enable **Override local DNS**

This gives you ad-blocking and local DNS resolution from anywhere on your tailnet.

## Troubleshooting

### "certificate is not valid for this name"

The hostname in the browser doesn't match the certificate's Subject Alternative Name. Make sure you're using the exact MagicDNS hostname:

```bash
openssl x509 -in /etc/dead-mans-switch/tls.crt -noout -ext subjectAltName
```

### "tailscale cert" fails with "HTTPS not enabled"

Enable HTTPS certificates in the [Tailscale admin console](https://login.tailscale.com/admin/dns) under **HTTPS Certificates**.

### "tailscale cert" fails with permission error

The Tailscale daemon needs root privileges:

```bash
sudo tailscale cert \
  --cert-file /etc/dead-mans-switch/tls.crt \
  --key-file /etc/dead-mans-switch/tls.key \
  my-server.tail-name.ts.net
```

### Push notifications not working

Push notifications require a secure context (HTTPS). Verify that:

1. You're accessing the app via `https://` (not `http://`)
2. The certificate is trusted by your browser (Tailscale certs are Let's Encrypt-issued, so they should be)
3. You're using the MagicDNS hostname, not a raw IP address

### Connection refused from another device

Ensure both devices are on the same tailnet and connected:

```bash
tailscale ping my-server.tail-name.ts.net
```

If the ping fails, check that Tailscale is running on both machines and that no ACL rules are blocking traffic.

## References

- [Tailscale HTTPS](https://tailscale.com/kb/1153/enabling-https)
- [Tailscale MagicDNS](https://tailscale.com/kb/1081/magicdns)
- [`tailscale cert` documentation](https://tailscale.com/kb/1153/enabling-https#use-tailscale-cert)
- [Let's Encrypt](https://letsencrypt.org/)
