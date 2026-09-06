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

The bridge logs in with a Beszel user's email/password on startup (and
again automatically if its session expires), so there's no token to mint
or rotate by hand.

### Recommended: create a read-only Beszel user for the bridge

Create a dedicated read-only user for the bridge, so its credentials
can't modify or delete anything in Beszel.

1. In PocketBase's admin panel (`http://<beszel-host>:8090/_/`), open the
   `users` collection, add a new user, and set its `role` field to
   **read-only**.
2. Give it visibility into the systems/containers the bridge needs to
   check: in the same admin panel, open the `systems` collection, open
   each system record, and add the read-only user in the `users` field.

A read-only user can't create or modify systems, only view what's shared
with it. The bridge only ever reads, so this is enough.

`docker-compose.yml`:
```yaml
services:
  beszel-bridge:
    image: ghcr.io/pingumania/beszel-bridge:latest
    restart: unless-stopped
    environment:
      - BESZEL_URL=http://beszel:8090
      - BESZEL_EMAIL=readonly@example.com
      - BESZEL_PASSWORD=yourpassword
    ports:
      - "8123:8123"
```

## Env variables

| Variable          | Required | Default               | Description                       |
|-------------------|----------|------------------------|------------------------------------|
| `BESZEL_URL`      | no       | `http://beszel:8090`  | Root URL of your Beszel instance   |
| `BESZEL_EMAIL`    | yes      | -                      | Email of the Beszel user (see Setup) |
| `BESZEL_PASSWORD` | yes      | -                      | Password of the Beszel user        |
| `PORT`            | no       | `8123`                 | Server listen port                 |

## Usage

Dashy `conf.yml`:
```yaml
statusCheckUrl: http://beszel-bridge:8123/status/<system_name>/<container_name>
```
