# syntax=docker/dockerfile:1
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /manager ./cmd

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
