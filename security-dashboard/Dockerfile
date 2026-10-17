# Build runs on the host platform; the final image is a static binary plus
# CA certificates, so a cross-platform build needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY web ./web
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/security-dashboard .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/security-dashboard /security-dashboard
USER 65532:65532
EXPOSE 8080 8081
ENTRYPOINT ["/security-dashboard"]
