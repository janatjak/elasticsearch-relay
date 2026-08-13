# elasticsearch-relay

HTTP relay pro Elasticsearch. Přijme jakýkoli požadavek (typicky `POST /{index}/_doc/{id}` a `POST /_bulk`), okamžitě vrátí `200` a požadavek zařadí do fronty v paměti. Worker frontu asynchronně přeposílá do Elasticsearch — aplikace tak nikdy nečeká na Elasticsearch a krátkodobý výpadek ES nezpůsobí ztrátu logů.

## Jak to funguje

- Každý příchozí požadavek se přepošle do Elasticsearch (`baseUrl` z argumentu) v původní podobě, včetně hlaviček. Při chybě spojení se opakuje (max 5×, s 10s pauzou).
- Pokud je nastavena `VICTORIALOGS_URL`, posílají se `POST /{index}/_doc/{id}` a `POST /_bulk` požadavky **navíc** i do VictoriaLogs:
  - jednotlivé `_doc` dokumenty se převádí na bulk formát,
  - ES pole se přejmenují na jejich VictoriaLogs ekvivalenty: `message` → `_msg`, `@timestamp` → `_time`,
  - pokud dokument `_msg` nemá odkud vzít (VictoriaLogs ho vyžaduje), doplní se `"missing _msg"`,
  - název ES indexu se do dokumentů doplní jako pole `index` a posílá se jako stream field (`?_stream_fields=index`),
  - vše se hromadí do jednoho payloadu a odesílá na `/insert/elasticsearch/_bulk` max. jednou za 30 s (nebo dřív při překročení 4 MB),
  - při nedostupnosti VictoriaLogs se neodeslaný buffer drží do 32 MB, pak se zahodí (best-effort).

## Spuštění

```bash
./elasticsearch-relay http://elasticsearch:9200
```

| Konfigurace | Význam |
| --- | --- |
| 1. argument | Base URL Elasticsearch (povinné) |
| `VICTORIALOGS_URL` | Base URL VictoriaLogs včetně případné basic auth, např. `https://user:pass@victorialogs:9428`. Bez cesty — relay si sám doplní `/insert/elasticsearch/_bulk`. Nepovinné. |
| `APP_DEBUG` | `1` = logování každého přijatého/odeslaného požadavku |
| `PORT` | Port HTTP serveru (výchozí `8080`) |

### Endpointy

- `GET /health-check` — vrací `200`
- `GET /info` — počet požadavků ve frontě + celkový počet od startu
- vše ostatní — zařazeno do fronty a přeposláno

## Docker

```dockerfile
FROM golang:alpine

RUN go install github.com/janatjak/elasticsearch-relay@v1.0.5

FROM alpine:3
RUN apk add --no-cache ca-certificates
COPY --from=0 /go/bin/elasticsearch-relay /usr/local/bin/elasticsearch-relay
EXPOSE 8080
ENTRYPOINT ["elasticsearch-relay"]
```

Verzi v `go install` upravte podle aktuálního tagu (podpora VictoriaLogs bude ve verzi novější než v1.0.5).

### docker compose

```yaml
services:
  elasticsearch-relay:
    build: .
    command: ["http://elasticsearch:9200"]
    environment:
      VICTORIALOGS_URL: "https://user:pass@victorialogs:9428"
      # APP_DEBUG: "1"
    ports:
      - "8080:8080"
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://localhost:8080/health-check"]
      interval: 30s
      timeout: 5s
```

Aplikační klienty (např. Monolog Elasticsearch handler) pak stačí nasměrovat na `http://elasticsearch-relay:8080` místo přímo na Elasticsearch.

## Vývoj

```bash
go build ./...
go test ./...
```
