# Multi-stage: build the frontend (cloned from its own repo -- no
# orchestrator/monorepo ties these together, same pattern as
# valve-stiction-detection installing valve-stiction-ml via
# `pip install git+https://...`), then the backend, then ship just the
# backend binary + frontend's static build output -- one image, matching
# "go service untuk serve fe".
FROM node:20-slim AS frontend-build
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
ARG FRONTEND_REPO=https://github.com/maulanaiskak/valve-stiction-frontend.git
ARG FRONTEND_REF=main
RUN git clone --depth 1 --branch "$FRONTEND_REF" "$FRONTEND_REPO" .
RUN npm ci && npm run build

FROM golang:1.25 AS backend-build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /backend .

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=backend-build /backend .
COPY --from=frontend-build /app/dist ./static
ENV STATIC_DIR=/app/static
EXPOSE 8080
ENTRYPOINT ["/app/backend"]
