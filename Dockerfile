FROM golang:1.17 as builder

WORKDIR /src

COPY . .

RUN go build -o indexer .

FROM golang:1.17 as app

WORKDIR /app

COPY --from=builder /src/indexer /app/indexer

ENTRYPOINT [ "/app/indexer" ]