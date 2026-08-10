FROM golang:1.25-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/gorouter ./cmd/gorouter

FROM alpine:3.20
RUN addgroup -S gorouter && adduser -S -G gorouter gorouter
COPY --from=build /out/gorouter /usr/local/bin/gorouter
USER gorouter
EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/gorouter"]
