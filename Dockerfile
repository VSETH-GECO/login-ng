FROM golang:1.25-alpine AS build

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY main.go .
COPY server server

RUN go build -o /login .

FROM alpine:3.22

WORKDIR /app

COPY --from=build /login .

COPY static static
COPY templates templates
COPY migrations migrations

EXPOSE 8080

ENTRYPOINT [ "/app/login" ]
