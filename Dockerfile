# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24-alpine AS web
RUN npm install -g pnpm@11
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web/*.go ./web/
COPY --from=web /src/web/build ./web/build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags embedui -trimpath \
	-ldflags="-s -w -X github.com/sobakintech/maitake/internal/version.Version=$VERSION" \
	-o /out/maitake ./cmd/maitake \
	&& mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/maitake /maitake
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV MAITAKE_HOST=0.0.0.0 \
	MAITAKE_DATA_DIR=/data
EXPOSE 8317
VOLUME /data
ENTRYPOINT ["/maitake", "-config", "/config.yaml"]
