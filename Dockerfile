# syntax=docker/dockerfile:1
FROM golang:1.21-alpine as build

WORKDIR /app

COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY *.go ./

ARG VERSION
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.Version=${VERSION}" -o /tired-manager

# FROM python:slim
FROM scratch

COPY --from=build /tired-manager /
COPY jobs /jobs

CMD [ "/tired-manager" ]