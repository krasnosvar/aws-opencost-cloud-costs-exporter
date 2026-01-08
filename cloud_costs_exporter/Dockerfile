FROM golang:1.25-alpine AS build
WORKDIR /src
COPY src/go.mod src/go.sum ./
RUN go mod download
COPY src/ ./
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /out/opencost-cloud-costs-exporter ./

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/opencost-cloud-costs-exporter /opencost-cloud-costs-exporter
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/opencost-cloud-costs-exporter"]
