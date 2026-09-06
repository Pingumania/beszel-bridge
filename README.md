# beszel-bridge

Translates a container's health status in a self-hosted
[Beszel](https://github.com/henrygd/beszel) instance into a plain HTTP
status code, for status-check widgets (e.g.
[Dashy](https://github.com/Lissy93/dashy)) that expect one.

Beszel's own API always returns 200 with a JSON body describing status,
which Dashy can't interpret. This bridge asks Beszel "is this container
healthy?" and answers with a plain HTTP status code instead:

| Code | Meaning                                      |
|------|-----------------------------------------------|
| 200  | container/system is up                         |
| 503  | container/system is down or unhealthy          |
| 404  | couldn't find a system/container with that name |
| 502  | couldn't reach Beszel at all                    |

## Setup

Get an auth token from your Beszel instance (needs your Beszel account
email/password once, to mint the token — the bridge itself never stores
them):

```sh
curl -s -X POST http://<beszel-host>:8090/api/collections/users/auth-with-password \
  -H "Content-Type: application/json" \
  -d '{"identity":"you@example.com","password":"yourpassword"}' | jq -r .token
```

`docker-compose.yml`:
```yaml
services:
  beszel-bridge:
    image: ghcr.io/pingumania/beszel-bridge:latest
    restart: unless-stopped
    environment:
      - BESZEL_URL=http://beszel:8090
      - BESZEL_TOKEN=eyJhbGciOi...
    ports:
      - "8123:8123"
```

PocketBase tokens expire (default 7 days unless your Beszel instance
configures a longer TTL) — re-run the curl above and update `BESZEL_TOKEN`
when the bridge starts returning 502s.

## Env variables

| Variable       | Required | Default               | Description                          |
|-----------------|----------|--------------------------|-----------------------------------------|
| `BESZEL_URL`   | no       | `http://beszel:8090`  | Root URL of your Beszel instance      |
| `BESZEL_TOKEN` | yes      | -                      | PocketBase auth token (see Setup)     |
| `PORT`         | no       | `8123`                 | Server listen port                    |

## Usage

Dashy `conf.yml`:
```yaml
statusCheckUrl: http://beszel-bridge:8123/status/<system_name>/<container_name>
```
