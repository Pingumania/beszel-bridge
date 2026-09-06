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

`docker-compose.yml`:
```yaml
services:
  beszel-bridge:
    image: ghcr.io/pingumania/beszel-bridge:latest
    restart: unless-stopped
    environment:
      - BESZEL_URL=http://beszel:8090
      - BESZEL_EMAIL=you@example.com
      - BESZEL_PASSWORD=yourpassword
    ports:
      - "8123:8123"
```

## Env variables

| Variable          | Required | Default                | Description                     |
|--------------------|----------|--------------------------|-----------------------------------|
| `BESZEL_URL`      | no       | `http://beszel:8090`   | Root URL of your Beszel instance |
| `BESZEL_EMAIL`    | yes      | -                       | Beszel account email             |
| `BESZEL_PASSWORD` | yes      | -                       | Beszel account password          |
| `PORT`            | no       | `8123`                  | Server listen port               |

## Usage

Dashy `conf.yml`:
```yaml
statusCheckUrl: http://beszel-bridge:8123/status/<system_name>/<container_name>
```
