# syntax=docker/dockerfile:1.7
# ──────────── build stage ────────────
FROM node:20-alpine AS build
WORKDIR /workspace

# Copy workspace manifests first for better layer caching.
COPY package.json package-lock.json* .npmrc tsconfig.base.json ./
COPY shared/package.json shared/tsconfig.json shared/
COPY core/package.json core/tsconfig.json core/vite.config.ts core/env.d.ts core/index.html core/
# e2e is not needed for this image, but its package.json must exist to satisfy workspaces.
COPY e2e/package.json e2e/

RUN npm install --no-fund --no-audit \
  && npm install --no-fund --no-audit --no-save "@rollup/rollup-linux-x64-musl@$(node -p "require('./node_modules/rollup/package.json').version")"

# Copy source.
COPY shared/src ./shared/src
COPY core/src ./core/src
COPY core/public ./core/public

RUN npm run build -w @authman/core

# ──────────── runtime stage ────────────
FROM nginx:1.27-alpine AS runtime

RUN rm /etc/nginx/conf.d/default.conf
COPY core/nginx.conf /etc/nginx/conf.d/default.conf

COPY --from=build /workspace/core/dist /usr/share/nginx/html

COPY core/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 8080
ENV AUTHMAN_API_BASE=/api \
    AUTHMAN_DEFAULT_LOCALE=en

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
