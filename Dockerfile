FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN go build -trimpath -ldflags="-s -w" -o /out/authman ./cmd/authman

FROM alpine:3.21
RUN adduser -D -H -u 10001 authman
COPY --from=build /out/authman /usr/local/bin/authman
USER authman
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/authman"]
