# Troubleshooting

## Quick Check

```bash
curl https://YOUR_DOMAIN/api/health
systemctl status sirius-api openresty redis-server
journalctl -u sirius-api --since "10 min ago"
```

## Common Issues

### "tunnel not found" for all requests
- Redis down: `systemctl start redis-server`
- Password mismatch: compare `REDIS_PASSWORD` in `/etc/sirius-agent/env` vs `/etc/redis/sirius-agent.conf`
- Test: `redis-cli -a PASSWORD ping`

### API auth errors
- Header must be: `Authorization: Bearer YOUR_KEY`
- Check key: `grep API_KEYS /etc/sirius-agent/env`

### OpenResty won't start
```bash
openresty -t -c /etc/sirius-agent/nginx/nginx.conf
ls -la /etc/sirius-agent/tls/          # certs must exist
ss -tlnp | grep -E ':80|:443'          # port conflicts
```

### Tunnels not proxying
```bash
curl -vk https://YOUR_UPSTREAM_HOST:PORT    # direct connectivity test
tail -50 /var/log/sirius-agent/nginx-error.log


```

### TLS issues
```bash
openssl x509 -enddate -noout -in /etc/sirius-agent/tls/fullchain.pem
certbot renew --dry-run
certbot renew --force-renewal && systemctl reload openresty
```

### Rate limiting too aggressive
- Proxy (default 600 req/min): set `PROXY_RATE_LIMIT_RPM` in `/etc/sirius-agent/env` → `systemctl restart openresty`
- API (default 30 req/min): set `RATE_LIMIT_RPM` in `/etc/sirius-agent/env` → `systemctl restart sirius-api`

> `rate_limit.lua` is not loaded by nginx - the active file is `tunnel_lookup.lua`.

### Template placeholders in nginx config
```bash
grep -r "__" /etc/sirius-agent/nginx/   # check for unsubstituted vars
bash install.sh                          # re-run installer to regenerate
```

### Logs filling disk
```bash
cat > /etc/logrotate.d/sirius-agent <<EOF
/var/log/sirius-agent/*.log {
    daily
    rotate 14
    compress
    missingok
    notifempty
    postrotate
        systemctl reload openresty 2>/dev/null || true
    endscript
}
EOF
```

## Useful Commands

```bash
journalctl -u sirius-api -f
tail -f /var/log/sirius-agent/nginx-access.log

redis-cli -a PASS KEYS "tunnel:*"
redis-cli -a PASS GET "tunnel:brave-fox-a3f1"
redis-cli -a PASS TTL "tunnel:brave-fox-a3f1"
redis-cli -a PASS DEL "tunnel:brave-fox-a3f1"

curl -s http://127.0.0.1:8181/api/health
openresty -t -c /etc/sirius-agent/nginx/nginx.conf
```
