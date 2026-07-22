# syntax=docker/dockerfile:1
FROM golang:1.26.5-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/gitops-local-render ./cmd/gitops-local-render

FROM gcr.io/distroless/static-debian12:nonroot
ARG SOURCE_URL
LABEL org.opencontainers.image.source="${SOURCE_URL}"
COPY --from=build /out/gitops-local-render /usr/local/bin/gitops-local-render
ENTRYPOINT ["/usr/local/bin/gitops-local-render"]
