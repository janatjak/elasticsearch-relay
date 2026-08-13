FROM golang:alpine

RUN go install github.com/janatjak/elasticsearch-relay@v1.0.5

FROM alpine:3
RUN apk add --no-cache ca-certificates
COPY --from=0 /go/bin/elasticsearch-relay /usr/local/bin/elasticsearch-relay
EXPOSE 8080
ENTRYPOINT ["elasticsearch-relay"]
