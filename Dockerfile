# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=docker
RUN CGO_ENABLED=0 go build \
	-ldflags "-s -w -X github.com/shaxzodbek-uzb/pgproof/internal/buildinfo.Version=${VERSION}" \
	-o /out/pgproof .

FROM alpine:3.20
# postgresql-client + mariadb-client provide pg_dump/pg_restore/psql + mysqldump/mysql.
RUN apk add --no-cache postgresql-client mariadb-client ca-certificates tzdata
COPY --from=build /out/pgproof /usr/local/bin/pgproof
# Default config location; mount your own over it.
ENV PGPROOF_CONFIG=/etc/pgproof/pgproof.yml
ENTRYPOINT ["pgproof"]
CMD ["--help"]
