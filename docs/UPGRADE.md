# Upgrade Guide

## Binary (most common)

```bash
# Rebuild
bash scripts/build.sh

# Deploy (builds + uploads + restarts)
bash scripts/deploy.sh root@YOUR_VPS

# Or manually
scp dist/sirius-api root@YOUR_VPS:/usr/local/bin/sirius-api
ssh root@YOUR_VPS "systemctl restart sirius-api"
```

Active tunnels survive restarts (they live in Redis).

## Lua Scripts

```bash
scp proxy/lua/tunnel_lookup.lua root@YOUR_VPS:/opt/sirius-agent/lua/
scp proxy/lua/rate_limit.lua root@YOUR_VPS:/opt/sirius-agent/lua/
ssh root@YOUR_VPS "systemctl reload openresty"
```

## Nginx Config

> **Warning**: configs have template variables (`__BASE_DOMAIN__`, `__UPSTREAM_URL__`). Don't copy them raw — re-run the installer instead:

```bash
cd /root/sirius-agent-src && bash install.sh
```

Or substitute manually:

```bash
DOMAIN="agent.example.com"
UPSTREAM_URL="https://upstream-server:8443"
DOMAIN_ESCAPED=$(echo "$DOMAIN" | sed 's/\./\\\\./g')

sed -e "s|__BASE_DOMAIN__|${DOMAIN}|g" \
    -e "s|__BASE_DOMAIN_ESCAPED__|${DOMAIN_ESCAPED}|g" \
    -e "s|__UPSTREAM_URL__|${UPSTREAM_URL}|g" \
    proxy/conf.d/proxy.conf > /etc/sirius-agent/nginx/conf.d/proxy.conf

openresty -t -c /etc/sirius-agent/nginx/nginx.conf && systemctl reload openresty
```

## Changing Upstream

```bash
nano /etc/sirius-agent/nginx/conf.d/proxy.conf   # update proxy_pass
openresty -t -c /etc/sirius-agent/nginx/nginx.conf && systemctl reload openresty

nano /etc/sirius-agent/env                        # update UPSTREAM_URL=
systemctl restart sirius-api
```

## API Key Rotation

1. `bash scripts/generate-key.sh`
2. Append to `API_KEYS=old_key,new_key` in `/etc/sirius-agent/env`
3. `systemctl restart sirius-api`
4. Update clients, then remove old key and restart again

## Redis Password Rotation

1. Update `REDIS_PASSWORD` in `/etc/sirius-agent/env`
2. Update `requirepass` in `/etc/redis/sirius-agent.conf`
3. Update `Environment=REDIS_PASSWORD=` in `/etc/systemd/system/openresty.service.d/sirius-agent.conf`
4. `systemctl daemon-reload && systemctl restart redis-server sirius-api openresty`
