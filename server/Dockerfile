FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/qoimeks-server ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -o /out/qoimeks-import ./cmd/import \
 && CGO_ENABLED=0 go build -trimpath -o /out/qoimeks-migrate-tenant ./cmd/migrate-tenant

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 qoimeks
COPY --from=build /out/ /usr/local/bin/
USER qoimeks
EXPOSE 8080
ENTRYPOINT ["qoimeks-server"]
